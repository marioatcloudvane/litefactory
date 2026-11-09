package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// linearBugE2E is a Linear project with a bug label, the real owner-action
// service with the bug_file handler, the real filing processor and start step
// over SQLite. Fakes: the Linear port and the Bug runtime hand-off.
type linearBugE2E struct {
	t         *testing.T
	db        *store.DB
	repo      store.Repo
	principal owneraction.Principal
	svc       *owneraction.Service
	linear    *fakeBugLinear
	d         *Daemon
	now       time.Time
	bindErr   []error
	admits    int
}

func newLinearBugE2E(t *testing.T, bugLabel string) *linearBugE2E {
	t.Helper()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "e2e.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	if _, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RegisterConnectorClient(ctx, store.ConnectorClient{ID: "client", Name: "Client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}, at); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", store.DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	ts := store.FormatTime(at)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES('grant','client','https://example.test/litefactory/mcp','repository:actions',42,?,?,?)`, []any{owner.Generation, ts, store.FormatTime(at.Add(24 * time.Hour))}},
		{`INSERT INTO connector_grant_repositories(grant_id,repo_id,repository_ref) VALUES('grant',?,'github:acme/api')`, []any{repo.ID}},
		{`INSERT INTO connector_grant_action_repositories(grant_id,repo_id,repository_ref) VALUES('grant',?,'github:acme/api')`, []any{repo.ID}},
	} {
		if _, err = raw.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "E2E", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: linearTestOrigin, Destination: linearTestDest, Enabled: true, Provenance: "explicit",
		Linear: store.LinearConfiguration{WorkspaceID: "w", TeamID: "team", ProjectID: "proj", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`, BugLabelID: bugLabel}}, 0); err != nil {
		t.Fatal(err)
	}
	e := &linearBugE2E{t: t, db: db, repo: repo, now: at,
		principal: owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client", GrantID: "grant"},
		linear:    &fakeBugLinear{issues: map[string]ticket.Snapshot{}, withLabel: true, publisher: ticket.Identity{Provider: ticket.Linear, Origin: linearTestOrigin, RemoteID: "pub-1"}}}
	protect := func(_ context.Context, s string) (string, error) { return s, nil }
	e.svc = owneraction.New(db, owneractions.Domain{BugFile: owneractions.BugFiles{DB: db, Labels: noGitHubLabels{}, Protect: protect, Availability: owneractions.Availability{BugFile: true}}, Availability: owneractions.Availability{BugFile: true}}, func() time.Time { return e.now })
	e.restart()
	e.stubStart()
	return e
}

// restart is a fresh Daemon value over the same database.
func (e *linearBugE2E) restart() {
	filter, err := turn.NewFilter(e.db, turn.Identities{OwnerUserID: 42, OwnerHandle: "owner", BotUserID: bugFilingTestBot}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.d = &Daemon{db: e.db, filter: filter, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	e.d.brainstormTasks.clock = func() time.Time { return e.now }
}

// stubStart replaces the start step's seams: the Linear admission and binding
// run on the real store with the fake Linear read.
func (e *linearBugE2E) stubStart() {
	t := e.t
	admit, bind := bugFilingLinearAdmit, bugFilingLinearBind
	t.Cleanup(func() {
		bugFilingLinearAdmit, bugFilingLinearBind = admit, bind
	})
	bugFilingLinearAdmit = func(d *Daemon, ctx context.Context, repo store.Repo, f store.BugFiling) (store.Session, ticket.Ref, error) {
		e.admits++
		s, err := e.linear.Resolve(ctx, ticket.Locator{Provider: ticket.Linear, Origin: linearTestOrigin, Container: linearTestDest, Key: f.IssueNodeID})
		if err != nil {
			return store.Session{}, ticket.Ref{}, err
		}
		if _, err = d.db.GetTicketBinding(ctx, s.Ref.Identity); errors.Is(err, store.ErrNotFound) {
			v, cerr := d.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
			if cerr != nil {
				return store.Session{}, ticket.Ref{}, cerr
			}
			_, err = d.db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: v.ID, Ref: s.Ref, Enabled: true, Provenance: "discovery"})
		}
		if err != nil {
			return store.Session{}, ticket.Ref{}, err
		}
		v, err := d.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
		if err != nil {
			return store.Session{}, ticket.Ref{}, err
		}
		slot, err := d.db.LocalTicketSlot(ctx, "issue", s.Ref.Identity)
		if err != nil {
			return store.Session{}, ticket.Ref{}, err
		}
		taskRef := naming.Ref{Forge: "linear", Owner: "team", Repo: "proj", Issue: int(slot)}.String()
		actor := store.PublishingActor{ID: 900, Login: "factory"}
		code, err := d.db.CurrentConnection(ctx, repo.ID, store.CodeConnection)
		if err != nil {
			return store.Session{}, ticket.Ref{}, err
		}
		session, err := d.db.AdmitSessionWithPublishers(ctx, repo, s.Ref, code.Revision, v.Revision, actor, actor, taskRef, 0, "101", "proj")
		if err != nil {
			err = fmt.Errorf("admit session: %w", err)
		}
		return session, s.Ref, err
	}
	bugFilingLinearBind = func(d *Daemon, ctx context.Context, sessionID int64, ref ticket.Ref) error {
		if len(e.bindErr) > 0 {
			err := e.bindErr[0]
			e.bindErr = e.bindErr[1:]
			return err
		}
		_, err := d.db.BindSessionTicket(ctx, sessionID, ref)
		return err
	}
}

// handoffs counts the Bug starts the filings enqueued.
func (e *linearBugE2E) handoffs() int {
	e.t.Helper()
	due, err := e.db.ListEligibleSchedules(e.t.Context(), e.now.Add(time.Hour), 100)
	if err != nil {
		e.t.Fatal(err)
	}
	n := 0
	for _, s := range due {
		if s.Payload == bugFilingWorkPayload {
			n++
		}
	}
	return n
}

func (e *linearBugE2E) processor() *bugFilingProcessor {
	return &bugFilingProcessor{
		db: e.db, forge: &fakeBugForge{},
		authority: func(context.Context, store.BugFiling) error { return nil },
		linear: func(context.Context, store.ConnectionVersion) (bugLinearTarget, error) {
			return bugLinearTarget{port: e.linear, publisher: e.linear.publisher, origin: linearTestOrigin}, nil
		},
		redactor: redact.New(redact.Options{}),
		bot:      bugFilingTestBot,
		now:      func() time.Time { return e.now },
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (e *linearBugE2E) propose(key string) (owneraction.Proposal, error) {
	return e.svc.Propose(e.t.Context(), e.principal, owneraction.Invocation{Command: owneractions.BugFileCommand,
		Arguments: map[string]string{"repository": "acme/api", "title": "Crash on save", "description": "Saving crashes."}}, key)
}

// round is one pass of the owner-action step, the filing processor and the
// start step, as the serving loops run them.
func (e *linearBugE2E) round(id string) owneraction.Proposal {
	e.t.Helper()
	got, err := e.svc.Step(e.t.Context(), id)
	if err != nil {
		e.t.Fatalf("action step: %v", err)
	}
	if err = e.processor().step(e.t.Context()); err != nil {
		e.t.Fatalf("processor: %v", err)
	}
	active, err := e.db.ActiveBugFilings(e.t.Context(), 10)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, f := range active {
		if f.State != store.BugFilingLabelled {
			continue
		}
		if err = e.d.enqueueBugFiling(e.t.Context(), f); err != nil {
			e.t.Logf("start step: %v", err)
		}
	}
	e.now = e.now.Add(30 * time.Second)
	return got
}

func TestBugFileLinearE2EProposeConfirmFilesOneIssueAndStartsBug(t *testing.T) {
	e := newLinearBugE2E(t, linearTestLabel)
	ctx := t.Context()

	proposal, err := e.propose("owner-bug-1")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := e.propose("owner-bug-1")
	if err != nil || retry.ID != proposal.ID {
		t.Fatalf("retry gave another proposal: %+v %v", retry.ID, err)
	}
	if e.linear.creates != 0 {
		t.Fatalf("a proposal alone created %d issues", e.linear.creates)
	}
	e.now = e.now.Add(time.Minute)
	if _, err = e.svc.Confirm(ctx, e.principal, proposal.ID); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 12; i++ {
		e.round(proposal.ID)
	}
	inspected, err := e.svc.Inspect(ctx, e.principal, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	filing, err := e.db.BugFilingByExecution(ctx, inspected.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}

	// One create, with the saved UUID and the label ID, no GitHub forge write.
	prep, err := e.db.GetTicketEffectPreparation(ctx, filing.EffectKey)
	if err != nil {
		t.Fatal(err)
	}
	if e.linear.creates != 1 || len(e.linear.gotLabelIDs) != 1 || e.linear.gotLabelIDs[0] != linearTestLabel || e.linear.labelWrites != 0 {
		t.Fatalf("creates=%d labels=%v label writes=%d", e.linear.creates, e.linear.gotLabelIDs, e.linear.labelWrites)
	}
	if _, ok := e.linear.issues[prep.CreationUUID]; !ok || filing.IssueNodeID != prep.CreationUUID {
		t.Fatalf("created uuid %q, filing %+v", prep.CreationUUID, filing)
	}

	// The link comes back through the inspected result; the filing ended started.
	link := "https://linear.app/t/issue/" + prep.CreationUUID
	if inspected.Result.State != "succeeded" || inspected.Result.Summary != "Bug started" || inspected.Result.Reference != link || filing.IssueLink != link {
		t.Fatalf("result %+v filing %+v", inspected.Result, filing)
	}
	if filing.State != store.BugFilingDispatched || filing.Tracker != store.BugTrackerLinear {
		t.Fatalf("filing %+v", filing)
	}

	// One session with the numeric alias, bound to the Linear issue; one hand-off.
	session, err := e.db.GetSession(ctx, filing.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	slot, err := e.db.LocalTicketSlot(ctx, "issue", ticket.Identity{Provider: ticket.Linear, Origin: linearTestOrigin, RemoteID: prep.CreationUUID})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := e.db.GetSessionTicket(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.IssueNumber < 1 || int64(session.IssueNumber) != slot || bound.RemoteID != prep.CreationUUID {
		t.Fatalf("session %+v slot %d bound %+v", session, slot, bound)
	}
	if e.handoffs() != 1 {
		t.Fatalf("hand-offs %d, want 1", e.handoffs())
	}

	// The repeated confirmation, the repeated proposal and more rounds change nothing.
	if _, err = e.svc.Confirm(ctx, e.principal, proposal.ID); err != nil {
		t.Fatalf("repeat confirm: %v", err)
	}
	again, err := e.propose("owner-bug-1")
	if err != nil || again.ID != proposal.ID {
		t.Fatalf("repeat proposal: %+v %v", again.ID, err)
	}
	for i := 0; i < 3; i++ {
		e.round(proposal.ID)
	}
	if e.linear.creates != 1 || e.handoffs() != 1 || e.admits != 1 {
		t.Fatalf("replay changed the filing: creates=%d handoffs=%d admits=%d", e.linear.creates, e.handoffs(), e.admits)
	}
	if latest, err := e.db.ReadBugFiling(ctx, filing.ID); err != nil || latest.State != store.BugFilingDispatched || latest.SessionID != session.ID {
		t.Fatalf("filing after replay %+v %v", latest, err)
	}
}

func TestBugFileLinearE2ERestartAfterCreateBeforeBindingRecoversOneSession(t *testing.T) {
	e := newLinearBugE2E(t, linearTestLabel)
	ctx := t.Context()
	proposal, err := e.propose("owner-bug-2")
	if err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	if _, err = e.svc.Confirm(ctx, e.principal, proposal.ID); err != nil {
		t.Fatal(err)
	}
	e.bindErr = []error{errors.New("crash before binding")}
	// Rounds until the issue is created, labelled and the start step crashed at binding.
	for i := 0; i < 8 && e.admits == 0; i++ {
		e.round(proposal.ID)
	}
	filing, err := e.db.BugFilingByExecution(ctx, "owner-action:"+proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.admits != 1 || filing.SessionID != 0 || e.handoffs() != 0 || e.linear.creates != 1 {
		t.Fatalf("before restart: admits=%d handoffs=%d creates=%d filing %+v", e.admits, e.handoffs(), e.linear.creates, filing)
	}

	e.restart()
	for i := 0; i < 6; i++ {
		e.round(proposal.ID)
	}
	filing, err = e.db.ReadBugFiling(ctx, filing.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionAgain, err := e.db.GetOrCreateSession(ctx, e.repo, filing.IssueNumber)
	if err != nil {
		t.Fatal(err)
	}
	if e.linear.creates != 1 || e.handoffs() != 1 || filing.SessionID == 0 || sessionAgain.ID != filing.SessionID || filing.State != store.BugFilingDispatched {
		t.Fatalf("after restart: creates=%d handoffs=%d filing %+v session %d", e.linear.creates, e.handoffs(), filing, sessionAgain.ID)
	}
	if bound, err := e.db.GetSessionTicket(ctx, filing.SessionID); err != nil || bound.RemoteID != filing.IssueNodeID {
		t.Fatalf("session not bound: %+v %v", bound, err)
	}
}

func TestBugFileLinearE2EProjectWithoutBugLabelIsRefused(t *testing.T) {
	e := newLinearBugE2E(t, "")
	_, err := e.propose("owner-bug-3")
	var refusal owneraction.Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "no bug label") {
		t.Fatalf("proposal = %v", err)
	}
	if e.linear.creates != 0 {
		t.Fatalf("creates=%d", e.linear.creates)
	}
}

// A fresh Linear-style issue labelled "bug" with no workflow admission is
// admitted as Bug; an issue that already has a retained admission keeps it.
func TestWorkflowAdmissionLinearBugLabel(t *testing.T) {
	setup := func(t *testing.T) (*Daemon, *boundaryAdmissionForge, store.Session) {
		t.Helper()
		db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err = db.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		session, err := db.GetOrCreateSession(t.Context(), repo, 21)
		if err != nil {
			t.Fatal(err)
		}
		forge := &boundaryAdmissionForge{issue: github.Issue{Number: 21, Labels: []string{"bug", "Backend"}}}
		return &Daemon{db: db, forge: forge}, forge, session
	}
	admit := func(t *testing.T, d *Daemon, s store.Session, trigger int64) (string, error) {
		w, _, err := d.admitWorkflow(t.Context(), &task{session: s, issue: github.Issue{Number: 21}}, turn.Trigger{CommentID: trigger}, turn.Command{})
		return string(w), err
	}

	t.Run("fresh issue with the bug label is admitted as Bug", func(t *testing.T) {
		d, forge, s := setup(t)
		if w, err := admit(t, d, s, 501); err != nil || w != "bug" {
			t.Fatalf("workflow %q %v", w, err)
		}
		a, err := d.db.GetTaskWorkflowAdmission(t.Context(), s.ID)
		if err != nil || a.Family != "standalone" || a.Workflow != "bug" || a.SourceTrigger != 501 || forge.reads != 1 {
			t.Fatalf("admission %+v reads=%d %v", a, forge.reads, err)
		}
	})
	t.Run("a retained admission is kept unchanged", func(t *testing.T) {
		d, forge, s := setup(t)
		if _, err := d.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 400}); err != nil {
			t.Fatal(err)
		}
		if w, err := admit(t, d, s, 501); err != nil || w != "bug" {
			t.Fatalf("workflow %q %v", w, err)
		}
		a, err := d.db.GetTaskWorkflowAdmission(t.Context(), s.ID)
		if err != nil || a.SourceTrigger != 400 || forge.reads != 0 {
			t.Fatalf("admission %+v reads=%d %v", a, forge.reads, err)
		}
	})
}

// noGitHubLabels fails the test path that would look for a GitHub label: a
// Linear project never consults it.
type noGitHubLabels struct{}

func (noGitHubLabels) RepoLabelNodeID(context.Context, github.Repo, string) (string, bool, error) {
	return "", false, errors.New("the GitHub label lookup must not run for a Linear project")
}

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func approveEffect(t *testing.T, db *store.DB, session store.Session, step store.DoctorRepairStep, evidence store.DoctorEvidence, source ...string) store.DoctorRepair {
	t.Helper()
	ctx := t.Context()
	sourceID := "owner-apply"
	if len(source) > 0 {
		sourceID = source[0]
	}
	prefix := "effects-" + sourceID
	_, proposal := doctorProposalFixture(t, db, session.TaskRef, prefix, prefix, step, evidence)
	if e := db.PresentDoctorProposal(ctx, session.TaskRef, prefix+"-presentation", proposal); e != nil {
		t.Fatal(e)
	}
	op, e := db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: prefix + "-repair", SourceID: sourceID, Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: prefix + "-presentation"})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AcknowledgeDoctorExclusion(ctx, op.ID, "factory"); e != nil {
		t.Fatal(e)
	}
	return op
}

// doctorProposalFixture records one accepted doctor attempt, made by sourceID,
// whose diagnosis proposes step; it returns the conversation and proposal.
func doctorProposalFixture(t *testing.T, db *store.DB, taskRef, prefix, sourceID string, step store.DoctorRepairStep, evidence store.DoctorEvidence) (store.DoctorConversation, int64) {
	t.Helper()
	ctx := t.Context()
	conversation, e := db.BeginDoctorAttempt(ctx, taskRef, store.DoctorAttempt{ID: prefix, SourceID: sourceID, OwnerText: "fixture", PromptVersion: doctor.PromptVersion, GuideVersion: doctor.GuideVersion})
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(evidence)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: prefix, Provider: "claude", ConfiguredModel: "fixture", Evidence: string(encoded)}); e != nil {
		t.Fatal(e)
	}
	binding, e := db.BindDoctorResultAttempt(ctx, prefix)
	if e != nil {
		t.Fatal(e)
	}
	diagnosis := doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "repair", Evidence: []string{step.Target}, Uncertainty: "fixture"}}, Proposal: []doctor.RepairStep{{Number: 1, Target: step.Target, Precondition: "exact", Change: "repair", Reason: "owner approved", Reversibility: "backup", Rollback: "restore", Action: &store.DoctorRepairAction{Kind: step.Kind, Before: step.Before, After: step.After}}}}
	snapshot, e := json.Marshal(diagnosis)
	if e != nil {
		t.Fatal(e)
	}
	manifest, e := json.Marshal(diagnosis.Proposal)
	if e != nil {
		t.Fatal(e)
	}
	current, e := db.GetResultRevision(ctx, binding, 0)
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = db.RecordResult(ctx, store.ResultRecord{Binding: binding, ExpectedRevision: current.Revision, RequestID: prefix + "-result", Payload: snapshot, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), Accepted: true}); e != nil {
		t.Fatal(e)
	}
	if e = db.AdoptDoctorResult(ctx, binding, prefix+"-result", manifest); e != nil {
		t.Fatal(e)
	}
	proposal, _, e := db.DoctorProposalReference(ctx, conversation.ID)
	if e != nil {
		t.Fatal(e)
	}
	return conversation, proposal
}

func TestFactoryDoctorConsumeConfirmedAndUncertain(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "lost-reply"}[uncertain], func(t *testing.T) {
			d, forge, repo := doctorDaemonFixture(t)
			ctx := t.Context()
			session, e := d.db.GetOrCreateSession(ctx, repo, 406)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = d.db.BootstrapWatermark(ctx, session.ID, 10); e != nil {
				t.Fatal(e)
			}
			if _, e = d.db.RecordComment(ctx, store.Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 7, Body: "repair", CreatedAt: d.now(), Classification: store.ClassificationInstruction}); e != nil {
				t.Fatal(e)
			}
			ref, e := naming.ParseRef(session.TaskRef)
			if e != nil {
				t.Fatal(e)
			}
			evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}
			step := store.DoctorRepairStep{Number: 1, Kind: "consume_comment", Target: "comment:11", Before: "10", After: "11"}
			op := approveEffect(t, d.db, session, step, evidence)
			if uncertain {
				forge.postErr = errors.New("reply lost")
			}
			adapter := FactoryDoctorRepairEffects{DB: d.db, Comments: forge, BotID: 99, Redactor: d.doctor.service.Redactor}
			result, e := adapter.Execute(ctx, ref, op, step, "claude")
			if uncertain {
				if e == nil {
					t.Fatal("lost receipt accepted")
				}
				wm, _, _ := d.db.GetWatermark(ctx, session.ID)
				if wm.LastCommentID != 10 {
					t.Fatal("unconfirmed reply consumed")
				}
				_, _ = adapter.Execute(ctx, ref, op, step, "claude")
				if len(forge.posts) != 1 {
					t.Fatal("uncertain reply replayed")
				}
			} else {
				if e != nil || result.State != "verified" || result.BackupReference == "" {
					t.Fatal(result, e)
				}
				if e = adapter.Verify(ctx, ref, result); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

// consumeFixture reserves an approved consume_comment step for comment 11.
func consumeFixture(t *testing.T, d *Daemon, repo store.Repo) (naming.Ref, store.DoctorRepair, store.DoctorRepairStep) {
	t.Helper()
	ctx := t.Context()
	session, e := d.db.GetOrCreateSession(ctx, repo, 406)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = d.db.BootstrapWatermark(ctx, session.ID, 10); e != nil {
		t.Fatal(e)
	}
	if _, e = d.db.RecordComment(ctx, store.Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 7, Body: "repair", CreatedAt: d.now(), Classification: store.ClassificationInstruction}); e != nil {
		t.Fatal(e)
	}
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	step := store.DoctorRepairStep{Number: 1, Kind: "consume_comment", Target: "comment:11", Before: "10", After: "11"}
	return ref, approveEffect(t, d.db, session, step, evidence), step
}

// C2, C3, C8, C9: a failed reply POST is posted once and categorized from the
// ticket failure kind the real transport wraps it in.
func TestFactoryDoctorConsumeReplyFailureCategory(t *testing.T) {
	api := func(status int, sentinel error) github.APIError {
		return github.APIError{Method: "POST", URL: "https://api.github.com/repos/acme/api/issues/406/comments", StatusCode: status, Err: sentinel}
	}
	publish := func(kind ticket.FailureKind, cause error) error {
		return &ticket.Failure{Kind: kind, Operation: "publish reply", Cause: cause}
	}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"rate limit 429", publish(ticket.Refused, &github.RateLimitError{APIError: api(429, github.ErrRateLimited), RetryAfter: time.Minute}), doctor.ErrRepairReplyRateLimited},
		{"secondary rate limit 403", publish(ticket.Refused, &github.RateLimitError{APIError: api(403, github.ErrRateLimited), RetryAfter: time.Minute}), doctor.ErrRepairReplyRateLimited},
		{"ticket provider quota", publish(ticket.Refused, &ticket.RateLimitError{RetryAfter: time.Minute}), doctor.ErrRepairReplyRateLimited},
		{"local hold deferral", publish(ticket.Uncertain, ticket.ErrDiscoveryDeferred), doctor.ErrRepairReplyRateLimited},
		{"forbidden 403", publish(ticket.Refused, &github.ForbiddenError{APIError: api(403, github.ErrForbidden)}), doctor.ErrRepairReplyRefused},
		{"not found 404", publish(ticket.Refused, &github.NotFoundError{APIError: api(404, github.ErrNotFound)}), doctor.ErrRepairReplyRefused},
		{"unprocessable 422", publish(ticket.Refused, &github.APIError{Method: "POST", StatusCode: 422, Err: github.ErrUnprocessable}), doctor.ErrRepairReplyRefused},
		{"transport uncertain", publish(ticket.Uncertain, &url.Error{Op: "Post", URL: "https://api.github.com/repos/acme/api/issues/406/comments", Err: errors.New("connection reset")}), doctor.ErrRepairReplyOutcomeUncertain},
		{"unclassified", errors.New("reply lost"), doctor.ErrRepairReplyOutcomeUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, forge, repo := doctorDaemonFixture(t)
			ref, op, step := consumeFixture(t, d, repo)
			forge.postErr = tc.err
			adapter := FactoryDoctorRepairEffects{DB: d.db, Comments: forge, BotID: 99, Redactor: d.doctor.service.Redactor}
			_, e := adapter.Execute(t.Context(), ref, op, step, "claude")
			if !errors.Is(e, tc.want) {
				t.Fatalf("category of %v = %v, want %v", tc.err, e, tc.want)
			}
			for _, other := range []error{doctor.ErrRepairReplyRateLimited, doctor.ErrRepairReplyRefused, doctor.ErrRepairReplyOutcomeUncertain} {
				if !errors.Is(other, tc.want) && errors.Is(e, other) {
					t.Fatalf("%v also categorized as %v", e, other)
				}
			}
			if !errors.Is(e, tc.err) {
				t.Fatalf("cause dropped: %v", e)
			}
			if len(forge.posts) != 1 {
				t.Fatalf("posts=%d, want exactly one", len(forge.posts))
			}
		})
	}
}

// C7: a 429 on the reply POST through the real connection transport arms the
// connection's backoff, and the step returns after one POST without waiting.
func TestFactoryDoctorConsumeRateLimitArmsConnectionBackoff(t *testing.T) {
	d, _, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	origin := ticket.Origin("https://api.github.com")
	connection, e := d.db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: origin, Destination: "acme/api", RemoteID: "202", CredentialSource: "env", CredentialEnv: "DOCTOR_REPLY", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if e != nil {
		t.Fatal(e)
	}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch {
		case r.URL.Path == "/user":
			value = map[string]any{"id": 99, "login": "publisher", "type": "Bot"}
		case r.URL.Path == "/repos/acme/api":
			value = map[string]any{"id": 202, "full_name": "acme/api", "has_issues": true}
		case r.URL.Path == "/repos/acme/api/issues/406":
			value = map[string]any{"id": 10406, "number": 406, "url": "https://api.github.com/repos/acme/api/issues/406", "state": "open", "user": map[string]any{"id": 55, "login": "creator"}}
		case r.URL.Path == "/repos/acme/api/issues/406/comments" && r.Method == http.MethodPost:
			posts.Add(1)
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			value = map[string]any{"message": "You have exceeded a secondary rate limit"}
		default:
			t.Errorf("unexpected provider destination %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	target, e := url.Parse(server.URL)
	if e != nil {
		t.Fatal(e)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, HTTPClient: &http.Client{Transport: approverLoopbackTransport{target: target}}, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	d.connections = factory
	factory.SetRequestContext(d.bindQuotaRequest)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d.quotas.tick(now)
	clients, e := factory.Clients(ctx, connection, 99)
	if e != nil {
		t.Fatal(e)
	}
	ref, op, step := consumeFixture(t, d, repo)
	ticketRef := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "10406"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: "acme/api", Key: "406"}}
	adapter := FactoryDoctorRepairEffects{DB: d.db, Comments: publishReplyComments{tickets: clients.Tickets, ref: ticketRef}, BotID: 99, Redactor: d.doctor.service.Redactor}
	started := time.Now()
	_, e = adapter.Execute(ctx, ref, op, step, "claude")
	if !errors.Is(e, doctor.ErrRepairReplyRateLimited) {
		t.Fatalf("rate-limited reply categorized as %v", e)
	}
	var failure *ticket.Failure
	if !errors.As(e, &failure) || failure.Kind != ticket.Refused {
		t.Fatalf("reply refusal not wrapped by the transport: %v", e)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("consume step waited %s", elapsed)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts=%d, want one", posts.Load())
	}
	scoped := context.WithValue(ctx, doctorSourceContextKey{}, store.DoctorCommandSource{ConnectionVersionID: connection.ID, PublisherID: 99, Ticket: ticketRef})
	until, held := d.connectionHold(scoped, now)
	if !held || until.Before(now.Add(2*time.Minute)) {
		t.Fatalf("connection backoff not armed from Retry-After: until=%s held=%v", until, held)
	}
}

// publishReplyComments posts through the real ticket adapter, as the routed
// publication bridge does.
type publishReplyComments struct {
	tickets interface {
		PublishReply(context.Context, ticket.Ref, redact.Body) (ticket.Reply, error)
	}
	ref ticket.Ref
}

func (p publishReplyComments) PostComment(ctx context.Context, _ github.Repo, _ int, body redact.Body) (github.Comment, error) {
	if _, err := p.tickets.PublishReply(ctx, p.ref, body); err != nil {
		return github.Comment{}, err
	}
	return github.Comment{}, errors.New("rate-limited fixture accepted a reply")
}

func TestFactoryDoctorBranchAdapterRecordsAndRechecks(t *testing.T) {
	d, _, _ := doctorDaemonFixture(t)
	ctx := t.Context()
	fixture := gittest.New(t)
	repo, e := d.db.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: "github", Owner: "fixture", Name: "repo", RemoteURL: fixture.Origin, BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	session, e := d.db.GetOrCreateSession(ctx, repo, 539)
	if e != nil {
		t.Fatal(e)
	}
	session, e = d.db.FreezeSessionSlug(ctx, session.ID, "sync", "lf/539-sync")
	if e != nil {
		t.Fatal(e)
	}
	binary, e := git.Resolve(ctx)
	if e != nil {
		t.Fatal(e)
	}
	clone, e := binary.Open(ctx, fixture.Dir)
	if e != nil {
		t.Fatal(e)
	}
	tree, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(repo.WorktreeRoot, "task-539"), Branch: session.Branch, BaseBranch: repo.BaseBranch})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.db.SetSessionWorktree(ctx, session.ID, tree.Worktree.Path()); e != nil {
		t.Fatal(e)
	}
	head, e := tree.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	evidence.Records = append(evidence.Records, store.DoctorEvidenceRecord{Kind: "branch", ID: session.Branch, State: head, Identity: head})
	step := store.DoctorRepairStep{Number: 1, Kind: "sync_branch", Target: "branch:" + session.Branch, Before: head, After: head}
	op := approveEffect(t, d.db, session, step, evidence)
	adapter := FactoryDoctorRepairEffects{DB: d.db, Git: binary, CloneGate: newCloneGate(), Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}}
	for _, bad := range []store.DoctorRepairStep{
		{Number: 99, Kind: step.Kind, Target: step.Target, Before: step.Before, After: step.After},
		{Number: 1, Kind: "consume_comment", Target: step.Target, Before: step.Before, After: step.After},
		{Number: 1, Kind: step.Kind, Target: step.Target, Before: "drift", After: step.After},
	} {
		if _, e := adapter.Execute(ctx, ref, op, bad, "claude"); e == nil {
			t.Fatal("unapproved manifest accepted", bad)
		}
	}
	result, e := adapter.Execute(ctx, ref, op, step, "claude")
	if e != nil {
		t.Fatal(e)
	}
	if result.State != "verified" || result.BackupReference != "git:head:"+head {
		t.Fatal(result)
	}
	if e = adapter.Verify(ctx, ref, result); e != nil {
		t.Fatal(e)
	}
	if _, e = adapter.Execute(context.Background(), ref, op, step, "claude"); e == nil {
		t.Fatal("verified external effect replay accepted")
	}
}

// finishRacingGate records the step as finished during the apply acquisition,
// so the adapter's own FinishDoctorBranchSync fails after a successful push.
type finishRacingGate struct {
	turn.CloneGate
	acquisitions int
	race         func()
}

func (g *finishRacingGate) Acquire(ctx context.Context, root string) (func(), error) {
	release, e := g.CloneGate.Acquire(ctx, root)
	if e == nil {
		if g.acquisitions++; g.acquisitions == 3 {
			g.race()
		}
	}
	return release, e
}

func TestFactoryDoctorBranchFinishFailureKeepsPushedHead(t *testing.T) {
	d, _, _ := doctorDaemonFixture(t)
	ctx := t.Context()
	fixture := gittest.New(t)
	repo, e := d.db.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: "github", Owner: "fixture", Name: "repo", RemoteURL: fixture.Origin, BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	session, e := d.db.GetOrCreateSession(ctx, repo, 539)
	if e != nil {
		t.Fatal(e)
	}
	session, e = d.db.FreezeSessionSlug(ctx, session.ID, "sync", "lf/539-sync")
	if e != nil {
		t.Fatal(e)
	}
	binary, e := git.Resolve(ctx)
	if e != nil {
		t.Fatal(e)
	}
	clone, e := binary.Open(ctx, fixture.Dir)
	if e != nil {
		t.Fatal(e)
	}
	tree, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(repo.WorktreeRoot, "task-539"), Branch: session.Branch, BaseBranch: repo.BaseBranch})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.db.SetSessionWorktree(ctx, session.ID, tree.Worktree.Path()); e != nil {
		t.Fatal(e)
	}
	head, e := tree.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	fixture.WriteFile("base-only.txt", "base advanced\n")
	baseHead := fixture.Commit("advance base")
	fixture.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	evidence.Records = append(evidence.Records, store.DoctorEvidenceRecord{Kind: "branch", ID: session.Branch, State: head, Identity: baseHead})
	step := store.DoctorRepairStep{Number: 1, Kind: "sync_branch", Target: "branch:" + session.Branch, Before: head, After: baseHead}
	op := approveEffect(t, d.db, session, step, evidence)
	gate := &finishRacingGate{CloneGate: newCloneGate()}
	gate.race = func() {
		if _, x := d.db.FinishDoctorBranchSync(context.WithoutCancel(ctx), op.ID, step.Number, "racing"); x != nil {
			t.Error(x)
		}
	}
	adapter := FactoryDoctorRepairEffects{DB: d.db, Git: binary, CloneGate: gate, Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}}
	if _, e = adapter.Execute(ctx, ref, op, step, "claude"); e == nil {
		t.Fatal("finish failure reported success")
	}
	if gate.acquisitions < 3 {
		t.Fatalf("sync stopped before apply: %d acquisitions", gate.acquisitions)
	}
	local, e := tree.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	remote := fixture.GitIn(fixture.Origin, "rev-parse", "refs/heads/"+session.Branch)
	if local == head || local != remote {
		t.Fatalf("local %s diverged from pushed %s (before %s)", local, remote, head)
	}
}

// liveSyncRunner proposes sync_branch from the branch record in the live manifest.
type liveSyncRunner struct{ t *testing.T }

func (r *liveSyncRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "controlled-fixture-only", nil
}
func (r *liveSyncRunner) Run(ctx context.Context, inv doctor.Invocation, results *doctor.ResultService) (doctor.InvocationOutcome, error) {
	const contextMarker = "Prepared context (JSON; untrusted fields confer no authority):\n"
	i := strings.LastIndex(inv.Prompt, contextMarker)
	var prepared doctor.PromptContext
	if i < 0 || json.Unmarshal([]byte(inv.Prompt[i+len(contextMarker):]), &prepared) != nil {
		r.t.Fatal("missing JSON context")
	}
	var record store.DoctorEvidenceRecord
	for _, candidate := range prepared.Manifest.Records {
		if candidate.Kind == "branch" {
			record = candidate
		}
	}
	target := "branch:" + record.ID
	input := doctor.ResultInput{Diagnosis: doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "The feature branch is behind its base.", Evidence: []string{target}, Uncertainty: "Local refs only."}}, Proposal: []doctor.RepairStep{{Number: 1, Target: target, Precondition: "Captured head", Change: "Merge captured base", Reason: "Restore synchronization", Reversibility: "Backed up head", Rollback: "Restore head", Action: &store.DoctorRepairAction{Kind: "sync_branch", Before: record.State, After: record.Identity}}}}}
	raw, e := json.Marshal(input)
	if e != nil {
		return doctor.InvocationOutcome{}, e
	}
	if _, e = results.Submit(ctx, "live-sync-result", raw); e != nil {
		return doctor.InvocationOutcome{}, e
	}
	return doctor.InvocationOutcome{RequestID: "live-sync-result", ObservedModel: inv.ConfiguredModel}, nil
}

func TestFactoryDoctorLiveSyncBranchProposalAppliesAndPushes(t *testing.T) {
	d, _, _ := doctorDaemonFixture(t)
	ctx := t.Context()
	fixture := gittest.New(t)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	repo, e := d.db.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: "github", Owner: "fixture", Name: "repo", RemoteURL: fixture.Origin, BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: root})
	if e != nil {
		t.Fatal(e)
	}
	session, e := d.db.GetOrCreateSession(ctx, repo, 655)
	if e != nil {
		t.Fatal(e)
	}
	if session, e = d.db.FreezeSessionSlug(ctx, session.ID, "sync", "lf/655-sync"); e != nil {
		t.Fatal(e)
	}
	binary, e := git.Resolve(ctx)
	if e != nil {
		t.Fatal(e)
	}
	clone, e := binary.Open(ctx, fixture.Dir)
	if e != nil {
		t.Fatal(e)
	}
	tree, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(repo.WorktreeRoot, "task-655"), Branch: session.Branch, BaseBranch: repo.BaseBranch})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.db.SetSessionWorktree(ctx, session.ID, tree.Worktree.Path()); e != nil {
		t.Fatal(e)
	}
	dir := tree.Worktree.Path()
	if e = os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature work\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	fixture.GitIn(dir, "add", "feature.txt")
	fixture.GitIn(dir, "commit", "--quiet", "-m", "feature work")
	fixture.GitIn(dir, "push", "--quiet", "origin", session.Branch)
	head := fixture.GitIn(dir, "rev-parse", "HEAD")
	fixture.WriteFile("base-only.txt", "base advanced\n")
	baseHead := fixture.Commit("advance base")
	fixture.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		t.Fatal(e)
	}
	d.doctor.service.Runner = &liveSyncRunner{t: t}
	got, e := d.doctor.service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: "Bring main into the feature branch", SourceID: "live-sync"})
	if e != nil {
		t.Fatal(e)
	}
	if len(got.CurrentProposal) != 1 || got.CurrentProposal[0].Action.Before != head || got.CurrentProposal[0].Action.After != baseHead {
		t.Fatalf("live sync_branch proposal not adopted: %+v", got.CurrentProposal)
	}
	if e = d.db.PresentDoctorProposal(ctx, session.TaskRef, "live-sync-presentation", got.ProposalID); e != nil {
		t.Fatal(e)
	}
	op, e := d.db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "live-sync-repair", SourceID: "owner-apply", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: "live-sync-presentation"})
	if e != nil {
		t.Fatalf("live sync_branch proposal not reservable: %v", e)
	}
	if e = d.db.AcknowledgeDoctorExclusion(ctx, op.ID, "factory"); e != nil {
		t.Fatal(e)
	}
	steps, e := d.db.DoctorRepairSteps(ctx, op.ID)
	if e != nil || len(steps) != 1 {
		t.Fatalf("repair steps: %+v %v", steps, e)
	}
	adapter := FactoryDoctorRepairEffects{DB: d.db, Git: binary, CloneGate: newCloneGate(), Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}}
	result, e := adapter.Execute(ctx, ref, op, steps[0], "claude")
	if e != nil {
		t.Fatal(e)
	}
	if e = adapter.Verify(ctx, ref, result); e != nil {
		t.Fatal(e)
	}
	pushed := fixture.GitIn(fixture.Origin, "rev-parse", "refs/heads/"+session.Branch)
	if pushed == head || pushed != result.ObservedAfter {
		t.Fatalf("pushed %s, before %s, result %+v", pushed, head, result)
	}
	fixture.GitIn(fixture.Origin, "merge-base", "--is-ancestor", baseHead, pushed)
	fixture.GitIn(fixture.Origin, "merge-base", "--is-ancestor", head, pushed)
}

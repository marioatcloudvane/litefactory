package daemon

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstormtasks"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const (
	linearTestOrigin = ticket.Origin("https://api.linear.app")
	linearTestDest   = "team/proj"
	linearTestLabel  = "lbl-bug"
)

type fakeBugLinear struct {
	mu          sync.Mutex
	issues      map[string]ticket.Snapshot
	withLabel   bool
	publisher   ticket.Identity
	creates     int
	labelWrites int
	gotLabelIDs []string
}

func (f *fakeBugLinear) snapshot(uuid, title, body string, labels []string) ticket.Snapshot {
	id := ticket.Identity{Provider: ticket.Linear, Origin: linearTestOrigin, RemoteID: uuid}
	return ticket.Snapshot{Ref: ticket.Ref{Identity: id, Locator: ticket.Locator{Provider: ticket.Linear, Origin: linearTestOrigin, Container: linearTestDest, Key: uuid}},
		BrowserURL: "https://linear.app/t/issue/" + uuid, Title: title, Body: body, ProviderBody: body, Representation: bugLinearRepresentation,
		Author: ticket.Actor{Identity: f.publisher}, Labels: labels}
}

// remote makes the issue exist as if a create had landed.
func (f *fakeBugLinear) remote(uuid, title, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues[uuid] = f.snapshot(uuid, title, body, []string{"bug"})
}

func (f *fakeBugLinear) CreateLabelledWithID(_ context.Context, _, id, labelID string, title, body redact.Body) (ticket.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	f.gotLabelIDs = append(f.gotLabelIDs, labelID)
	var labels []string
	if f.withLabel {
		labels = []string{"bug"}
	}
	s := f.snapshot(id, title.String(), body.String(), labels)
	f.issues[id] = s
	return s, nil
}

func (f *fakeBugLinear) Resolve(_ context.Context, l ticket.Locator) (ticket.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.issues[l.Key]; ok {
		return s, nil
	}
	return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Unavailable, Operation: "read"}
}

func (f *fakeBugLinear) AddLabel(_ context.Context, ref ticket.Ref, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labelWrites++
	s := f.issues[ref.Identity.RemoteID]
	s.Labels = append(s.Labels, "bug")
	f.issues[ref.Identity.RemoteID] = s
	return nil
}

type linearFilingFixture struct {
	db     *store.DB
	linear *fakeBugLinear
	now    time.Time
	filing store.BugFiling
}

func newLinearFilingFixture(t *testing.T) *linearFilingFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "bug.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var revision int64
	if cur, curErr := db.CurrentConnection(ctx, repo.ID, store.TicketConnection); curErr == nil {
		revision = cur.Revision
	}
	if _, err = db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: linearTestOrigin, Destination: linearTestDest, Enabled: true, Provenance: "explicit",
		Linear: store.LinearConfiguration{WorkspaceID: "w", TeamID: "team", ProjectID: "proj", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`, BugLabelID: linearTestLabel}}, revision); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	f := &linearFilingFixture{db: db, now: now, linear: &fakeBugLinear{issues: map[string]ticket.Snapshot{}, withLabel: true, publisher: ticket.Identity{Provider: ticket.Linear, Origin: linearTestOrigin, RemoteID: "pub-1"}}}
	f.filing, _, err = db.AdmitBugFiling(ctx, store.BugFiling{ExecutionID: "owner-action:oa1", OwnerActionID: "oa1", OwnerID: "42", OwnerGeneration: 1, RepositoryID: repo.ID, RepositoryRef: "github:acme/api", Title: "Crash on save", Body: "Saving crashes.\n\nFiled for the owner from a chat client.",
		Tracker: store.BugTrackerLinear, EffectKey: "bug-filing:owner-action:oa1", LabelID: linearTestLabel, Destination: linearTestDest}, now)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *linearFilingFixture) step(t *testing.T) store.BugFiling {
	t.Helper()
	p := &bugFilingProcessor{
		db: f.db, forge: &fakeBugForge{},
		authority: func(context.Context, store.BugFiling) error { return nil },
		linear: func(context.Context, store.ConnectionVersion) (bugLinearTarget, error) {
			return bugLinearTarget{port: f.linear, publisher: f.linear.publisher, origin: linearTestOrigin}, nil
		},
		redactor: redact.New(redact.Options{}),
		bot:      bugFilingTestBot,
		now:      func() time.Time { return f.now },
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := p.step(t.Context()); err != nil {
		t.Logf("step: %v", err)
	}
	got, err := f.db.ReadBugFiling(t.Context(), f.filing.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// prepare saves the creation UUID as the processor does before any write.
func (f *linearFilingFixture) prepare(t *testing.T) string {
	t.Helper()
	loc := ticket.Locator{Provider: ticket.Linear, Origin: linearTestOrigin, Container: linearTestDest, Key: "creation:" + f.filing.EffectKey}
	if _, err := f.db.BindTicketEffect(t.Context(), f.filing.EffectKey, loc, f.linear.publisher); err != nil {
		t.Fatal(err)
	}
	prep, err := f.db.PrepareTicketEffect(t.Context(), f.filing.EffectKey, bugLinearRepresentation, f.filing.Title+"\n"+f.filing.Body, true)
	if err != nil {
		t.Fatal(err)
	}
	return prep.CreationUUID
}

// begun records the durable creating state a crash would leave behind.
func (f *linearFilingFixture) begun(t *testing.T) {
	t.Helper()
	next := f.filing
	next.State, next.AttemptAt = store.BugFilingCreating, f.now
	if _, err := f.db.SaveBugFiling(t.Context(), next, store.BugFilingAdmitted, f.now); err != nil {
		t.Fatal(err)
	}
}

func TestBugFilingLinearCreatesOnceLabelledAtCreation(t *testing.T) {
	t.Parallel()
	f := newLinearFilingFixture(t)
	got := f.step(t)
	if got.State != store.BugFilingCreated || got.IssueNodeID == "" || got.IssueNumber < 1 || got.IssueID != int64(got.IssueNumber) || got.IssueLink == "" {
		t.Fatalf("after create: %+v", got)
	}
	if f.linear.creates != 1 || !slices.Equal(f.linear.gotLabelIDs, []string{linearTestLabel}) {
		t.Fatalf("creates=%d labels=%v", f.linear.creates, f.linear.gotLabelIDs)
	}
	f.now = f.now.Add(time.Second)
	if got = f.step(t); got.State != store.BugFilingLabelled || f.linear.labelWrites != 0 || f.linear.creates != 1 {
		t.Fatalf("after label: %+v writes=%d", got, f.linear.labelWrites)
	}
}

func TestBugFilingLinearCrashAfterUUIDBeforeCreateSendsNoCreate(t *testing.T) {
	t.Parallel()
	f := newLinearFilingFixture(t)
	uuid := f.prepare(t)
	f.begun(t)
	if got := f.step(t); got.State != store.BugFilingUncertain {
		t.Fatalf("restart: %+v", got)
	}
	f.now = f.now.Add(brainstormtasks.CheckSchedule[0])
	if got := f.step(t); got.State != store.BugFilingUncertain {
		t.Fatalf("not found yet: %+v", got)
	}
	f.linear.remote(uuid, f.filing.Title, f.filing.Body)
	f.now = f.now.Add(24 * time.Hour)
	got := f.step(t)
	if got.State != store.BugFilingCreated || got.IssueNodeID != uuid || f.linear.creates != 0 {
		t.Fatalf("found by uuid: %+v creates=%d", got, f.linear.creates)
	}
}

func TestBugFilingLinearCrashAfterCreateBeforeRecordNoSecondCreate(t *testing.T) {
	t.Parallel()
	f := newLinearFilingFixture(t)
	uuid := f.prepare(t)
	f.begun(t)
	f.linear.remote(uuid, f.filing.Title, f.filing.Body)
	f.step(t)
	f.now = f.now.Add(brainstormtasks.CheckSchedule[0])
	got := f.step(t)
	if got.State != store.BugFilingCreated || f.linear.creates != 0 {
		t.Fatalf("recovered: %+v creates=%d", got, f.linear.creates)
	}
	f.now = f.now.Add(time.Second)
	if got = f.step(t); got.State != store.BugFilingLabelled || f.linear.creates != 0 {
		t.Fatalf("labelled: %+v creates=%d", got, f.linear.creates)
	}
}

func TestBugFilingLinearMissingLabelFallsBackToIDOnlyAdd(t *testing.T) {
	t.Parallel()
	f := newLinearFilingFixture(t)
	f.linear.withLabel = false
	if got := f.step(t); got.State != store.BugFilingCreated {
		t.Fatalf("create: %+v", got)
	}
	f.now = f.now.Add(time.Second)
	got := f.step(t)
	if got.State != store.BugFilingLabelled || f.linear.labelWrites != 1 || f.linear.creates != 1 {
		t.Fatalf("label: %+v writes=%d creates=%d", got, f.linear.labelWrites, f.linear.creates)
	}
}

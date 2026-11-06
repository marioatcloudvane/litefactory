package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstormtasks"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

const bugFilingTestBot int64 = 900

type fakeBugForge struct {
	mu sync.Mutex
	// createErr answers the next creates in order; nil entries create.
	createErr []error
	// createVisible decides whether an errored create still made the issue.
	createVisible bool
	issues        []bugIssue
	labelMissing  bool
	labelErr      []error
	// labelLands applies an errored label write anyway.
	labelLands bool

	creates, lists, reads, labelReads, labelWrites int
	writtenLabelIDs                                [][]string
}

func (f *fakeBugForge) CreateIssue(_ context.Context, _, title, body string) (bugIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	var err error
	if len(f.createErr) > 0 {
		err, f.createErr = f.createErr[0], f.createErr[1:]
	}
	if err != nil && !f.createVisible {
		return bugIssue{}, err
	}
	n := len(f.issues) + 1
	issue := bugIssue{ID: int64(1000 + n), NodeID: "I_node" + string(rune('0'+n)), Number: n, Title: title, Body: body, Link: "https://github.com/acme/api/issues/" + string(rune('0'+n)), AuthorID: bugFilingTestBot, CreatedAt: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	f.issues = append(f.issues, issue)
	return issue, err
}

func (f *fakeBugForge) ListBotIssues(context.Context, string, time.Time) ([]bugIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	return slices.Clone(f.issues), nil
}

func (f *fakeBugForge) ReadIssue(_ context.Context, _ string, number int) (bugIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	for _, issue := range f.issues {
		if issue.Number == number {
			return issue, nil
		}
	}
	return bugIssue{}, github.ErrNotFound
}

func (f *fakeBugForge) LabelNodeID(_ context.Context, _, name string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labelReads++
	if f.labelMissing || name != "bug" {
		return "", false, nil
	}
	return "LA_bug", true, nil
}

func (f *fakeBugForge) AddLabelsByID(_ context.Context, node string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labelWrites++
	f.writtenLabelIDs = append(f.writtenLabelIDs, slices.Clone(ids))
	var err error
	if len(f.labelErr) > 0 {
		err, f.labelErr = f.labelErr[0], f.labelErr[1:]
	}
	if err == nil || f.labelLands {
		for i := range f.issues {
			if f.issues[i].NodeID == node && slices.Contains(ids, "LA_bug") {
				f.issues[i].Labels = append(f.issues[i].Labels, "bug")
			}
		}
	}
	return err
}

func (f *fakeBugForge) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates + f.labelWrites
}

type bugFilingFixture struct {
	db      *store.DB
	forge   *fakeBugForge
	now     time.Time
	revoked bool
	checks  int
	filing  store.BugFiling
}

func newBugFilingFixture(t *testing.T) *bugFilingFixture {
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
	f := &bugFilingFixture{db: db, forge: &fakeBugForge{}, now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	marker := "<!-- lf:bug-filing v=1 marker=0123456789abcdef0123456789abcdef -->"
	f.filing, _, err = db.AdmitBugFiling(ctx, store.BugFiling{ExecutionID: "owner-action:oa1", OwnerActionID: "oa1", OwnerID: "42", OwnerGeneration: 1, RepositoryID: repo.ID, RepositoryRef: "github:acme/api", Title: "Crash on save", Body: "Saving crashes.\n\nFiled for the owner from a chat client.\n\n" + marker, Marker: marker}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// processor is a fresh processor, as a restarted daemon builds one.
func (f *bugFilingFixture) processor() *bugFilingProcessor {
	return &bugFilingProcessor{
		db:    f.db,
		forge: f.forge,
		authority: func(context.Context, store.BugFiling) error {
			f.checks++
			if f.revoked {
				return errBugFilingRevoked
			}
			return nil
		},
		bot: bugFilingTestBot,
		now: func() time.Time { return f.now },
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (f *bugFilingFixture) step(t *testing.T) store.BugFiling {
	t.Helper()
	if err := f.processor().step(t.Context()); err != nil {
		t.Logf("step: %v", err)
	}
	got, err := f.db.ReadBugFiling(t.Context(), f.filing.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (f *bugFilingFixture) advance(d time.Duration) { f.now = f.now.Add(d) }

func TestBugFilingCreatesThenLabelsByID(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	got := f.step(t)
	if got.State != store.BugFilingCreated || got.IssueNumber != 1 || got.IssueNodeID != "I_node1" || got.IssueLink == "" {
		t.Fatalf("after create: %+v", got)
	}
	f.advance(time.Second)
	got = f.step(t)
	if got.State != store.BugFilingLabelled || got.IssueLink != "https://github.com/acme/api/issues/1" {
		t.Fatalf("after label: %+v", got)
	}
	if f.forge.creates != 1 || f.forge.labelWrites != 1 || f.checks != 2 {
		t.Fatalf("creates=%d labels=%d checks=%d", f.forge.creates, f.forge.labelWrites, f.checks)
	}
	if !slices.Equal(f.forge.writtenLabelIDs[0], []string{"LA_bug"}) || !slices.Contains(f.forge.issues[0].Labels, "bug") {
		t.Fatalf("label write was not by ID: %+v", f.forge.writtenLabelIDs)
	}
	f.advance(time.Hour)
	if again := f.step(t); again.State != store.BugFilingLabelled || f.forge.writes() != 2 {
		t.Fatalf("labelled filing wrote again: %+v writes=%d", again, f.forge.writes())
	}
}

func TestBugFilingCrashBetweenCreateAndLabelResumesWithoutSecondCreate(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	if got := f.step(t); got.State != store.BugFilingCreated {
		t.Fatalf("after create: %+v", got)
	}
	// A restart builds a new processor from the durable record only.
	f.advance(10 * time.Second)
	got := f.step(t)
	if got.State != store.BugFilingLabelled || f.forge.creates != 1 || f.forge.labelWrites != 1 {
		t.Fatalf("resume: %+v creates=%d labels=%d", got, f.forge.creates, f.forge.labelWrites)
	}
}

func TestBugFilingCrashDuringCreateReconcilesByMarker(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	// The create was begun durably and the issue landed, then the factory stopped.
	begun := f.filing
	begun.State = store.BugFilingCreating
	begun.AttemptAt = f.now
	if ok, err := f.db.SaveBugFiling(t.Context(), begun, store.BugFilingAdmitted, f.now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := f.forge.CreateIssue(t.Context(), "", f.filing.Title, f.filing.Body); err != nil {
		t.Fatal(err)
	}
	f.forge.creates = 0
	if got := f.step(t); got.State != store.BugFilingUncertain || f.forge.creates != 0 {
		t.Fatalf("restart: %+v creates=%d", got, f.forge.creates)
	}
	f.advance(brainstormtasks.CheckSchedule[0])
	got := f.step(t)
	if got.State != store.BugFilingCreated || got.IssueNumber != 1 || got.IssueNodeID != "I_node1" || f.forge.creates != 0 {
		t.Fatalf("adopt: %+v creates=%d", got, f.forge.creates)
	}
}

func TestBugFilingUnclearCreateAdoptsByMarkerNeverSecondCreate(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.forge.createErr = []error{errors.New("connection reset")}
	f.forge.createVisible = true
	got := f.step(t)
	if got.State != store.BugFilingUncertain || got.IssueID != 0 {
		t.Fatalf("unclear: %+v", got)
	}
	// Not due yet: no listing.
	f.advance(time.Second)
	f.step(t)
	if f.forge.lists != 0 {
		t.Fatalf("checked early: %d", f.forge.lists)
	}
	// Revocation does not stop read-only reconciliation.
	f.revoked = true
	f.advance(brainstormtasks.CheckSchedule[0])
	got = f.step(t)
	if got.State != store.BugFilingCreated || got.IssueNumber != 1 || got.IssueLink == "" || f.forge.creates != 1 {
		t.Fatalf("adopt: %+v creates=%d", got, f.forge.creates)
	}
	f.advance(time.Second)
	got = f.step(t)
	if got.State != store.BugFilingLabelFailed || got.Reason != "authority was revoked; filed as "+got.IssueLink+", not labelled" || got.IssueLink == "" || f.forge.labelWrites != 0 || f.forge.creates != 1 {
		t.Fatalf("revoked label: %+v writes=%d", got, f.forge.writes())
	}
}

func TestBugFilingChecksExhaustedFailsUnconfirmed(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.forge.createErr = []error{errors.New("timeout")}
	if got := f.step(t); got.State != store.BugFilingUncertain {
		t.Fatalf("unclear: %+v", got)
	}
	var got store.BugFiling
	for range brainstormtasks.CheckSchedule {
		f.advance(15 * time.Minute)
		got = f.step(t)
	}
	if got.State != store.BugFilingFailedUnconfirmed || got.Reason != bugReasonUnconfirmed || got.ChecksDone != len(brainstormtasks.CheckSchedule) {
		t.Fatalf("exhausted: %+v", got)
	}
	if f.forge.creates != 1 || f.forge.lists != len(brainstormtasks.CheckSchedule) {
		t.Fatalf("creates=%d lists=%d", f.forge.creates, f.forge.lists)
	}
}

func TestBugFilingRateLimitRetriesWithinBound(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	limited := &github.RateLimitError{APIError: github.APIError{StatusCode: 403, Err: github.ErrRateLimited}, RetryAfter: time.Minute}
	f.forge.createErr = []error{limited}
	got := f.step(t)
	if got.State != store.BugFilingAdmitted || got.CreateRetries != 1 || got.Reason != bugReasonRateLimited {
		t.Fatalf("limited: %+v", got)
	}
	f.advance(30 * time.Second)
	f.step(t)
	if f.forge.creates != 1 {
		t.Fatalf("retried before the wait: %d", f.forge.creates)
	}
	f.advance(31 * time.Second)
	got = f.step(t)
	if got.State != store.BugFilingCreated || f.forge.creates != 2 || f.checks != 2 {
		t.Fatalf("retry: %+v creates=%d checks=%d", got, f.forge.creates, f.checks)
	}

	g := newBugFilingFixture(t)
	g.forge.createErr = []error{limited, limited, limited, limited}
	for range brainstormtasks.MaxRateLimitRetries + 1 {
		got = g.step(t)
		g.advance(2 * time.Minute)
	}
	if got.State != store.BugFilingNotCreated || got.Reason != bugReasonRateLimitedDone || g.forge.creates != brainstormtasks.MaxRateLimitRetries+1 {
		t.Fatalf("bound: %+v creates=%d", got, g.forge.creates)
	}
}

func TestBugFilingRevokedBeforeCreateSendsNothing(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.revoked = true
	got := f.step(t)
	if got.State != store.BugFilingNotCreated || got.Reason != bugReasonRevokedNothing || f.forge.writes() != 0 || f.checks != 1 {
		t.Fatalf("revoked: %+v writes=%d checks=%d", got, f.forge.writes(), f.checks)
	}
}

func TestBugFilingRevokedBeforeCreateRetrySendsNothing(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.forge.createErr = []error{&github.RateLimitError{APIError: github.APIError{StatusCode: 429, Err: github.ErrRateLimited}, RetryAfter: time.Second}}
	if got := f.step(t); got.State != store.BugFilingAdmitted {
		t.Fatalf("limited: %+v", got)
	}
	f.revoked = true
	f.advance(time.Minute)
	got := f.step(t)
	if got.State != store.BugFilingNotCreated || got.Reason != bugReasonRevokedNothing || f.forge.creates != 1 {
		t.Fatalf("revoked retry: %+v creates=%d", got, f.forge.creates)
	}
}

func TestBugFilingRevokedBeforeLabelKeepsLink(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	if got := f.step(t); got.State != store.BugFilingCreated {
		t.Fatalf("create: %+v", got)
	}
	f.revoked = true
	f.advance(time.Second)
	got := f.step(t)
	if got.State != store.BugFilingLabelFailed || got.IssueLink != "https://github.com/acme/api/issues/1" || got.Reason != "authority was revoked; filed as https://github.com/acme/api/issues/1, not labelled" {
		t.Fatalf("revoked label: %+v", got)
	}
	if f.forge.labelWrites != 0 || f.forge.creates != 1 || f.forge.reads == 0 {
		t.Fatalf("writes after revocation or no re-read: creates=%d labels=%d reads=%d", f.forge.creates, f.forge.labelWrites, f.forge.reads)
	}
}

func TestBugFilingRevokedBeforeLabelRetryRereadsOnly(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.step(t)
	f.forge.labelErr = []error{errors.New("bad gateway")}
	f.advance(time.Second)
	got := f.step(t)
	if got.State != store.BugFilingCreated || got.Reason != bugReasonLabelUnclear || got.LabelRetries != 1 {
		t.Fatalf("unclear label: %+v", got)
	}
	f.revoked = true
	f.advance(bugLabelRetry)
	got = f.step(t)
	if got.State != store.BugFilingLabelFailed || f.forge.labelWrites != 1 || got.IssueLink == "" {
		t.Fatalf("revoked retry: %+v labels=%d", got, f.forge.labelWrites)
	}
}

func TestBugFilingUnclearLabelAcceptedOnReread(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.step(t)
	f.forge.labelErr = []error{errors.New("timeout")}
	f.forge.labelLands = true
	f.advance(time.Second)
	got := f.step(t)
	if got.State != store.BugFilingLabelled || f.forge.labelWrites != 1 {
		t.Fatalf("reread: %+v labels=%d", got, f.forge.labelWrites)
	}
}

func TestBugFilingMissingLabelBoundedThenLabelFailed(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.step(t)
	f.forge.labelMissing = true
	var got store.BugFiling
	for i := range bugLabelAttempts {
		f.advance(bugLabelRetry)
		got = f.step(t)
		if i < bugLabelAttempts-1 && (got.State != store.BugFilingCreated || got.Reason != bugReasonLabelMissing) {
			t.Fatalf("missing label attempt %d: %+v", i, got)
		}
	}
	if got.State != store.BugFilingLabelFailed || got.IssueLink == "" || f.forge.labelWrites != 0 || f.forge.creates != 1 {
		t.Fatalf("label failed: %+v labels=%d creates=%d", got, f.forge.labelWrites, f.forge.creates)
	}
}

func TestBugFilingLabelErrorsBoundedThenLabelFailed(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	f.step(t)
	for range bugLabelAttempts {
		f.forge.labelErr = append(f.forge.labelErr, errors.New("server error"))
	}
	var got store.BugFiling
	for range bugLabelAttempts {
		f.advance(bugLabelRetry)
		got = f.step(t)
	}
	if got.State != store.BugFilingLabelFailed || f.forge.labelWrites != bugLabelAttempts || f.forge.creates != 1 || f.checks != 1+bugLabelAttempts {
		t.Fatalf("bounded: %+v labels=%d creates=%d checks=%d", got, f.forge.labelWrites, f.forge.creates, f.checks)
	}
	for _, ids := range f.forge.writtenLabelIDs {
		if !slices.Equal(ids, []string{"LA_bug"}) {
			t.Fatalf("label write not by ID: %+v", ids)
		}
	}
}

func TestBugFilingAuthorityRefusesForeignExecution(t *testing.T) {
	t.Parallel()
	f := newBugFilingFixture(t)
	d := &Daemon{db: f.db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.brainstormTasks.clock = func() time.Time { return f.now }
	if err := d.bugFilingAuthority(t.Context(), f.filing); !errors.Is(err, errBugFilingRevoked) {
		t.Fatalf("missing owner action: %v", err)
	}
	bad := f.filing
	bad.ExecutionID = "other"
	if err := d.bugFilingAuthority(t.Context(), bad); !errors.Is(err, errBugFilingRevoked) {
		t.Fatalf("foreign execution: %v", err)
	}
}

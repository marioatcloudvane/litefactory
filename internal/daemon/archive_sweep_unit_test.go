package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type archiveFixture struct {
	db      *store.DB
	reg     store.Repo
	dir     string
	lockDir string
	logs    bytes.Buffer
	now     time.Time
	get     func(context.Context, github.Repo, int) (github.Issue, error)
}

func newArchiveFixture(t *testing.T) *archiveFixture {
	t.Helper()
	x := &archiveFixture{lockDir: t.TempDir(), dir: filepath.Join(t.TempDir(), "archive"), now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	var err error
	x.db, err = store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.db.Close() })
	if err = x.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	x.reg, err = x.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	x.get = closedIssue
	return x
}

func closedIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	return github.Issue{ID: int64(n) * 10, Number: n, State: "closed", APIURL: fmt.Sprintf("https://api.github.com/repos/acme/api/issues/%d", n)}, nil
}

// owner creates a session with comments rows, the archivable data.
func (x *archiveFixture) owner(t *testing.T, issue, comments int) store.Session {
	t.Helper()
	s, err := x.db.GetOrCreateSession(t.Context(), x.reg, issue)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= comments; i++ {
		_, err := x.db.RecordComment(t.Context(), store.Comment{SessionID: s.ID, ForgeCommentID: int64(i), AuthorHandle: "alice", Body: "hello", CreatedAt: x.now, RecordedAt: x.now, Classification: store.ClassificationInstruction})
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (x *archiveFixture) daemon(opts ArchiveOptions) *Daemon {
	if opts.Dir == "" {
		opts.Dir = x.dir
	}
	if opts.Issues == nil {
		opts.Issues = retirementSweepIssueFunc(func(ctx context.Context, r github.Repo, n int) (github.Issue, error) { return x.get(ctx, r, n) })
	}
	return &Daemon{db: x.db, instanceID: "archive-test", log: slog.New(slog.NewJSONHandler(&x.logs, nil)), archive: opts,
		retirement: RetirementExecutionOptions{CloneGate: newCloneGate(), LockDir: x.lockDir}}
}

func (x *archiveFixture) run(t *testing.T, d *Daemon, now time.Time) error {
	t.Helper()
	return x.runCtx(t.Context(), t, d, now)
}

func (x *archiveFixture) runCtx(ctx context.Context, t *testing.T, d *Daemon, now time.Time) error {
	t.Helper()
	sweepSeed(d, now)
	return d.RunArchiveBatch(ctx, now)
}

func (x *archiveFixture) state(t *testing.T) store.ArchiveSweep {
	t.Helper()
	s, err := x.db.GetArchiveSweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (x *archiveFixture) comments(t *testing.T, id int64) int {
	t.Helper()
	c, err := x.db.ListComments(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return len(c)
}

func (x *archiveFixture) files(t *testing.T) (final, partial []string) {
	t.Helper()
	_ = filepath.WalkDir(x.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // Deliberate: the fixture lists what it can read; a vanished entry is not a test failure.
		}
		if strings.HasSuffix(p, archive.PartialSuffix) {
			partial = append(partial, p)
		} else if strings.HasSuffix(p, archive.FileSuffix) {
			final = append(final, p)
		}
		return nil
	})
	return final, partial
}

func TestRunArchiveBatch_Disabled(t *testing.T) {
	t.Parallel()
	x := newArchiveFixture(t)
	x.owner(t, 1, 2)
	d := x.daemon(ArchiveOptions{})
	d.archive.Dir = ""
	if err := x.run(t, d, x.now); err != nil {
		t.Fatal(err)
	}
	if s := x.state(t); s.ID != "" {
		t.Fatalf("disabled archive created state %+v", s)
	}
	d = x.daemon(ArchiveOptions{})
	d.retirement.CloneGate = nil
	if err := x.run(t, d, x.now); err != nil || x.state(t).ID != "" {
		t.Fatalf("archive ran without retirement coordination: %v", err)
	}
}

func TestRunArchiveBatch_OneOwnerPerTickThenDailyDue(t *testing.T) {
	t.Parallel()
	x := newArchiveFixture(t)
	a := x.owner(t, 11, 3)
	b := x.owner(t, 12, 2)
	d := x.daemon(ArchiveOptions{})

	if err := x.run(t, d, x.now); err != nil {
		t.Fatal(err)
	}
	s := x.state(t)
	if s.Complete || s.Cursor != a.ID || s.Owners != 1 || s.Counts[ArchiveOutcomeArchived] != 1 || !s.DeletedAny {
		t.Fatalf("after first tick: %+v", s)
	}
	if x.comments(t, a.ID) != 0 || x.comments(t, b.ID) != 2 {
		t.Fatalf("tick touched wrong owners: a=%d b=%d", x.comments(t, a.ID), x.comments(t, b.ID))
	}
	final, partial := x.files(t)
	if len(final) != 1 || len(partial) != 0 {
		t.Fatalf("files final=%v partial=%v", final, partial)
	}
	wantName := archive.BuildName(11, x.now)
	if filepath.Base(final[0]) != wantName || filepath.Dir(final[0]) != archive.TaskDir(x.dir, "github", "acme", "api", 11) {
		t.Fatalf("file %s, want %s in task dir", final[0], wantName)
	}
	c, err := archive.Verify(final[0])
	if err != nil || c.Trailer.Counts["comments"] != 3 {
		t.Fatalf("verify %+v, %v", c.Trailer, err)
	}
	if fi, _ := os.Stat(final[0]); fi.Mode().Perm() != archive.FileMode {
		t.Fatalf("file mode %v", fi.Mode().Perm())
	}
	if di, _ := os.Stat(filepath.Dir(final[0])); di.Mode().Perm() != archive.DirMode {
		t.Fatalf("dir mode %v", di.Mode().Perm())
	}
	if _, err := x.db.GetSession(t.Context(), a.ID); err != nil {
		t.Fatalf("skeleton session lost: %v", err)
	}

	if err := x.run(t, d, x.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s = x.state(t); s.Cursor != b.ID || s.Owners != 2 || s.Complete || x.comments(t, b.ID) != 0 {
		t.Fatalf("after second tick: %+v", s)
	}

	doneAt := x.now.Add(2 * time.Minute)
	if err := x.run(t, d, doneAt); err != nil {
		t.Fatal(err)
	}
	done := x.state(t)
	if !done.Complete || !done.NextDue.Equal(doneAt.Add(24*time.Hour)) {
		t.Fatalf("completion %+v", done)
	}
	if !strings.Contains(x.logs.String(), `"msg":"archive sweep complete"`) || !strings.Contains(x.logs.String(), `"compacted":true`) {
		t.Fatalf("missing completion summary with compaction: %s", x.logs.String())
	}

	if err := x.run(t, d, done.NextDue.Add(-time.Nanosecond)); err != nil || x.state(t).ID != done.ID {
		t.Fatalf("pass restarted before 24h: %v", err)
	}
	if err := x.run(t, d, done.NextDue); err != nil {
		t.Fatal(err)
	}
	if x.state(t).ID == done.ID {
		t.Fatal("new pass did not start at exact 24h boundary")
	}
}

func TestRunArchiveBatch_Outcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		comments    int
		issue       func(context.Context, github.Repo, int) (github.Issue, error)
		wantOutcome string
		wantDeleted bool
		wantDefer   int
	}{
		{"closed with rows/archived", 2, closedIssue, ArchiveOutcomeArchived, true, 0},
		{"no rows/forget cannot verify run logs/deferred", 0, closedIssue, ArchiveOutcomeDeferred, false, 1},
		{"issue still open/kept", 2, func(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
			i, _ := closedIssue(ctx, r, n)
			i.State = "open"
			return i, nil
		}, ArchiveOutcomeKept, false, 1},
		{"issue url for other repo/kept", 2, func(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
			i, _ := closedIssue(ctx, r, n)
			i.APIURL = "https://api.github.com/repos/evil/api/issues/1"
			return i, nil
		}, ArchiveOutcomeKept, false, 1},
		{"issue lookup fails/deferred", 2, func(context.Context, github.Repo, int) (github.Issue, error) {
			return github.Issue{}, errors.New("forge unavailable")
		}, ArchiveOutcomeDeferred, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			x := newArchiveFixture(t)
			x.get = tt.issue
			s := x.owner(t, 5, tt.comments)
			d := x.daemon(ArchiveOptions{})
			if err := x.run(t, d, x.now); err != nil {
				t.Fatal(err)
			}
			got := x.state(t)
			if got.Counts[tt.wantOutcome] != 1 || got.Cursor != s.ID || got.Owners != 1 || got.Deferred != tt.wantDefer || got.DeletedAny != tt.wantDeleted {
				t.Fatalf("state %+v", got)
			}
			final, partial := x.files(t)
			wantRows, wantFiles := tt.comments, 0
			if tt.wantDeleted {
				wantRows, wantFiles = 0, 1
			}
			if x.comments(t, s.ID) != wantRows || len(final) != wantFiles || len(partial) != 0 {
				t.Fatalf("comments=%d final=%v partial=%v", x.comments(t, s.ID), final, partial)
			}
		})
	}
}

func TestRunArchiveBatch_LockedOwnerIsKept(t *testing.T) {
	t.Parallel()
	x := newArchiveFixture(t)
	s := x.owner(t, 5, 2)
	if ok, err := x.db.AcquireSessionLock(t.Context(), s.ID, "other-worker"); err != nil || !ok {
		t.Fatalf("lock %v %v", ok, err)
	}
	if err := x.run(t, x.daemon(ArchiveOptions{}), x.now); err != nil {
		t.Fatal(err)
	}
	got := x.state(t)
	if got.Counts[ArchiveOutcomeKept] != 1 || x.comments(t, s.ID) != 2 {
		t.Fatalf("state %+v comments=%d", got, x.comments(t, s.ID))
	}
}

func TestRunArchiveBatch_ShutdownLeavesOwnerForNextRun(t *testing.T) {
	t.Parallel()
	x := newArchiveFixture(t)
	s := x.owner(t, 5, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := x.daemon(ArchiveOptions{Issues: retirementSweepIssueFunc(func(context.Context, github.Repo, int) (github.Issue, error) {
		cancel()
		return github.Issue{}, context.Canceled
	})})

	err := x.runCtx(ctx, t, d, x.now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	got := x.state(t)
	if got.Cursor != 0 || got.Owners != 0 || len(got.Counts) != 0 {
		t.Fatalf("cancelled visit advanced the cursor: %+v", got)
	}
	if final, partial := x.files(t); len(final)+len(partial) != 0 || x.comments(t, s.ID) != 2 {
		t.Fatalf("shutdown changed data: %v %v comments=%d", final, partial, x.comments(t, s.ID))
	}
	// The next run picks the same owner up and archives it.
	if err := x.run(t, x.daemon(ArchiveOptions{}), x.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x.comments(t, s.ID) != 0 || x.state(t).Counts[ArchiveOutcomeArchived] != 1 {
		t.Fatal("owner not archived after restart")
	}
}

func TestRunArchiveBatch_FailureDefersWithDatabaseUntouched(t *testing.T) {
	t.Parallel()
	boom := errors.New("injected failure")
	tests := []struct {
		name string
		opts func(t *testing.T, x *archiveFixture) ArchiveOptions
	}{
		{"failure before delete/file removed and rows kept", func(*testing.T, *archiveFixture) ArchiveOptions {
			return ArchiveOptions{BeforeDelete: func(context.Context) error { return boom }}
		}},
		{"archive root unwritable/rows kept", func(t *testing.T, x *archiveFixture) ArchiveOptions {
			blocker := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return ArchiveOptions{Dir: blocker}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			x := newArchiveFixture(t)
			s := x.owner(t, 5, 2)
			d := x.daemon(tt.opts(t, x))
			if err := x.run(t, d, x.now); err != nil {
				t.Fatal(err)
			}
			got := x.state(t)
			if got.Counts[ArchiveOutcomeDeferred] != 1 || got.Deferred != 1 || got.DeletedAny || got.Cursor != s.ID {
				t.Fatalf("state %+v", got)
			}
			if x.comments(t, s.ID) != 2 {
				t.Fatalf("rows were deleted: %d", x.comments(t, s.ID))
			}
			if final, partial := x.files(t); len(final)+len(partial) != 0 {
				t.Fatalf("leftover files final=%v partial=%v", final, partial)
			}
		})
	}
}

func TestRunArchiveBatch_PassStartCleansPartialsAndRunsSteps(t *testing.T) {
	t.Parallel()
	x := newArchiveFixture(t)
	leftover := filepath.Join(x.dir, "github", "acme", "api", "9", archive.BuildName(9, x.now)+archive.PartialSuffix)
	if err := os.MkdirAll(filepath.Dir(leftover), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leftover, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	var order []string
	d := x.daemon(ArchiveOptions{StartSteps: []ArchiveStep{
		{Name: "failing", Run: func(context.Context, *ArchivePass) error {
			order = append(order, "failing")
			return errors.New("step failed")
		}},
		{Name: "counting", Run: func(_ context.Context, p *ArchivePass) error {
			order = append(order, "counting")
			p.Sweep.Counts["expired"]++
			if _, err := os.Stat(leftover); !os.IsNotExist(err) {
				t.Errorf("partial file survived until step: %v", err)
			}
			return nil
		}},
	}})
	if err := x.run(t, d, x.now); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "failing,counting" {
		t.Fatalf("step order %v", order)
	}
	if s := x.state(t); !s.Complete || s.Counts["expired"] != 1 {
		t.Fatalf("state %+v (failing step must not block the pass)", s)
	}
	if err := x.run(t, d, x.now.Add(time.Hour)); err != nil || len(order) != 2 {
		t.Fatalf("steps reran inside one completed pass window: %v %v", err, order)
	}
}

func TestRunArchiveBatch_BackoffHoldSkips(t *testing.T) {
	t.Parallel()
	x := newArchiveFixture(t)
	x.owner(t, 5, 2)
	d := x.daemon(ArchiveOptions{})
	d.backoff.fail(x.now, time.Minute)
	if err := x.run(t, d, x.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if x.state(t).ID != "" {
		t.Fatal("archive ran during backoff hold")
	}
}

// Issue #1029 C2: the archive sweep shares the retirement reader, so a closed
// Linear-bound owner resolves its state instead of deferring on lookup.
func TestArchiveOwner_LinearProjectConnection(t *testing.T) {
	x := newRetirementLinearFixture(t)
	x.setState("completed")
	_, err := x.db.RecordComment(t.Context(), store.Comment{SessionID: x.session.ID, ForgeCommentID: 1, AuthorHandle: "alice", Body: "hello", CreatedAt: x.now, RecordedAt: x.now, Classification: store.ClassificationInstruction})
	if err != nil {
		t.Fatal(err)
	}
	x.d.archive = ArchiveOptions{Dir: filepath.Join(t.TempDir(), "archive")}
	visit, err := x.d.archiveOwner(x.ctx(t), x.session.ID, x.now, retirementTicketIssues{d: x.d})
	if visit.Reason == "issue_lookup_failed" || visit.Reason == "issue_not_confirmed_closed" {
		t.Fatalf("Linear archive owner did not resolve closure: %+v %v", visit, err)
	}
	if err != nil || visit.Outcome == ArchiveOutcomeDeferred {
		t.Fatalf("archive owner: %+v %v", visit, err)
	}
}

package daemon

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type sweepRealFixture struct {
	db           *store.DB
	dbPath       string
	repo         *gittest.Repo
	registration store.Repo
	client       *github.Client
	logs         bytes.Buffer
	calls        atomic.Int32
	status       atomic.Int32
	now          time.Time
	lockDir      string
	onRead       func()
}

func newSweepRealFixture(t *testing.T) *sweepRealFixture {
	t.Helper()
	x := &sweepRealFixture{repo: gittest.New(t), dbPath: filepath.Join(t.TempDir(), "state.db"), lockDir: t.TempDir(), now: time.Now().UTC().Add(40 * 24 * time.Hour)}
	var err error
	x.db, err = store.Open(t.Context(), x.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.db.Close() })
	if err = x.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	x.registration, err = x.db.UpsertRepo(t.Context(), store.Repo{Path: x.repo.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: x.repo.Origin, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(filepath.Dir(x.repo.Dir), "tasks")})
	if err != nil {
		t.Fatal(err)
	}
	x.status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x.calls.Add(1)
		if x.onRead != nil {
			x.onRead()
		}
		var number int
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/acme/api/issues/%d", &number); err != nil {
			t.Error(err)
		}
		if r.Header.Get("If-None-Match") != "" {
			t.Error("conditional final observation")
		}
		w.Header().Set("Content-Type", "application/json")
		status := int(x.status.Load())
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"id":%d,"number":%d,"state":"closed","url":"https://api.github.com/repos/acme/api/issues/%d"}`, number*10, number, number)
	}))
	t.Cleanup(server.Close)
	x.client, err = github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func (x *sweepRealFixture) owner(t *testing.T, n int) store.Session {
	t.Helper()
	s, err := x.db.GetOrCreateSession(t.Context(), x.registration, n)
	if err != nil {
		t.Fatal(err)
	}
	s, err = x.db.FreezeSessionSlug(t.Context(), s.ID, "retire", fmt.Sprintf("lf/%d-retire", n))
	if err != nil {
		t.Fatal(err)
	}
	s.WorktreePath = filepath.Join(x.registration.WorktreeRoot, fmt.Sprintf("task-%d", n))
	if err = x.db.SetSessionWorktree(t.Context(), s.ID, s.WorktreePath); err != nil {
		t.Fatal(err)
	}
	x.repo.Git("worktree", "add", "-b", s.Branch, s.WorktreePath, "main")
	x.repo.Git("push", "origin", s.Branch)
	return s
}
func (x *sweepRealFixture) daemon() *Daemon {
	return &Daemon{db: x.db, instanceID: "sweep-test", log: slog.New(slog.NewJSONHandler(&x.logs, nil)), retirement: RetirementExecutionOptions{Issues: x.client, CloneGate: newCloneGate(), LockDir: x.lockDir, RetainDays: 14}}
}
func sweepSeed(d *Daemon, now time.Time) {
	d.discovery.tick(now)
	d.discovery.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(time.Hour)})
}
func (x *sweepRealFixture) state(t *testing.T) store.RetirementSweep {
	t.Helper()
	s, err := x.db.GetRetirementSweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func sweepRun(t *testing.T, d *Daemon, now time.Time) {
	t.Helper()
	sweepSeed(d, now)
	if err := d.RunRetirementBatch(t.Context(), now); err != nil {
		t.Fatal(err)
	}
}

// US-003 AC-001/002/003/005: durable real SQLite restart, exact clock boundary,
// fair bounded owner traversal, real Git removals, and uncached HTTP closure.
func TestRetirementSweepRealRestartAndDailyBoundary(t *testing.T) {
	x := newSweepRealFixture(t)
	first := x.owner(t, 213)
	second := x.owner(t, 214)
	d := x.daemon()
	sweepRun(t, d, x.now)
	s := x.state(t)
	if s.Complete || s.Cursor != first.ID || s.Owners != 1 {
		t.Fatalf("first batch %+v", s)
	}
	if _, err := os.Stat(first.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("first checkout survived: %v", err)
	}
	if _, err := os.Stat(second.WorktreePath); err != nil {
		t.Fatalf("batch touched second owner: %v", err)
	}
	if err := x.db.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	x.db, err = store.Open(t.Context(), x.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	d = x.daemon()
	sweepRun(t, d, x.now.Add(time.Minute))
	resumed := x.state(t)
	if resumed.ID != s.ID || resumed.Cursor != second.ID || resumed.Owners != 2 {
		t.Fatalf("restart lost progress: %+v", resumed)
	}
	completedAt := x.now.Add(2 * time.Minute)
	sweepRun(t, d, completedAt)
	done := x.state(t)
	if !done.Complete || !done.NextDue.Equal(completedAt.Add(24*time.Hour)) || done.Counts["removed"] != 6 {
		t.Fatalf("completed state %+v", done)
	}
	sweepRun(t, d, done.NextDue.Add(-time.Nanosecond))
	if x.state(t).ID != done.ID {
		t.Fatal("started before 24-hour boundary")
	}
	sweepRun(t, d, done.NextDue)
	if x.state(t).ID == done.ID {
		t.Fatal("did not start at exact boundary")
	}
	if !strings.Contains(x.logs.String(), `"msg":"retirement sweep complete"`) || !strings.Contains(x.logs.String(), `"completed":true`) {
		t.Fatal("missing visible completion summary")
	}
}
func TestRetirementSweepRealZeroCandidates(t *testing.T) {
	x := newSweepRealFixture(t)
	sweepRun(t, x.daemon(), x.now)
	s := x.state(t)
	if !s.Complete || s.Owners != 0 || s.ID == "" || x.calls.Load() != 0 {
		t.Fatalf("zero pass %+v calls=%d", s, x.calls.Load())
	}
	if !strings.Contains(x.logs.String(), `"owners":0`) {
		t.Fatal("missing zero-candidate summary")
	}
}
func TestRetirementSweepRealFailureFairness(t *testing.T) {
	for _, reason := range []string{"http_failure", "task_contention"} {
		t.Run(reason, func(t *testing.T) {
			x := newSweepRealFixture(t)
			a := x.owner(t, 213)
			b := x.owner(t, 214)
			d := x.daemon()
			if reason == "http_failure" {
				x.status.Store(http.StatusNotFound)
			} else {
				if ok, err := x.db.AcquireSessionLock(t.Context(), a.ID, "other-worker"); err != nil || !ok {
					t.Fatalf("lock %v %v", ok, err)
				}
			}
			sweepRun(t, d, x.now)
			s := x.state(t)
			if s.Cursor != a.ID || s.Deferred != 1 {
				t.Fatalf("failure did not advance %+v", s)
			}
			x.status.Store(http.StatusOK)
			sweepRun(t, d, x.now.Add(time.Minute))
			if s = x.state(t); s.Cursor != b.ID || s.Owners != 2 {
				t.Fatalf("later owner starved %+v", s)
			}
			if _, err := os.Stat(a.WorktreePath); err != nil {
				t.Fatal("failed owner deleted")
			}
			if _, err := os.Stat(b.WorktreePath); !os.IsNotExist(err) {
				t.Fatal("later owner not retired")
			}
		})
	}
}
func TestRetirementSweepRealBudgetAndBackoff(t *testing.T) {
	x := newSweepRealFixture(t)
	x.owner(t, 213)
	d := x.daemon()
	if err := d.RunRetirementBatch(t.Context(), x.now); err != nil {
		t.Fatal(err)
	}
	if s := x.state(t); s.Cursor != 0 || s.Complete || x.calls.Load() != 0 {
		t.Fatalf("unbudgeted pass %+v", s)
	}
	sweepSeed(d, x.now)
	d.backoff.fail(x.now, time.Minute)
	if err := d.RunRetirementBatch(t.Context(), x.now); err != nil {
		t.Fatal(err)
	}
	if x.calls.Load() != 0 {
		t.Fatal("backoff made requests")
	}
	sweepRun(t, d, x.now.Add(time.Minute))
	if x.state(t).Owners != 1 {
		t.Fatal("backoff boundary did not resume")
	}
}

// A real clone lock blocks the executor until cancellation; the sweep must keep
// its cursor and durable ownership, and a later process can retry that owner.
func TestRetirementSweepRealShutdownDuringCloneContention(t *testing.T) {
	x := newSweepRealFixture(t)
	s := x.owner(t, 213)
	d := x.daemon()
	sweepSeed(d, x.now)
	gate := newCloneGate()
	release, err := gate.Acquire(t.Context(), x.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	reached := make(chan struct{})
	d.retirement.CloneGate = sweepNotifyGate{gate, reached}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.RunRetirementBatch(ctx, x.now) }()
	select {
	case <-reached:
	case <-time.After(testutil.Wait):
		t.Fatal("executor never reached clone gate")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown reported success")
		}
	case <-time.After(testutil.Wait):
		t.Fatal("shutdown blocked")
	}
	state := x.state(t)
	if state.Complete || state.Cursor != 0 {
		t.Fatalf("shutdown advanced %+v", state)
	}
	if !strings.Contains(x.logs.String(), `"completed":false`) {
		t.Fatal("missing incomplete summary")
	}
	owner, err := x.db.GetRetirementOwner(t.Context(), s.ID)
	if err != nil || owner.Session.LockOwner != "" {
		t.Fatalf("lock leaked %+v %v", owner, err)
	}
	sweepRun(t, x.daemon(), x.now.Add(time.Minute))
	if x.state(t).Owners != 1 {
		t.Fatal("restart could not retry")
	}
}

type sweepNotifyGate struct {
	gate    *cloneGate
	reached chan struct{}
}

func (g sweepNotifyGate) Acquire(ctx context.Context, path string) (func(), error) {
	close(g.reached)
	return g.gate.Acquire(ctx, path)
}

// US-003 AC-003/005: a failed intent transaction leaves an incomplete durable
// sweep and cannot remove resources or claim a successful pass.
func TestRetirementSweepRealStorageFailure(t *testing.T) {
	x := newSweepRealFixture(t)
	owner := x.owner(t, 213)
	d := x.daemon()
	sweepSeed(d, x.now)
	x.onRead = func() { _ = x.db.Close() }
	err := d.RunRetirementBatch(t.Context(), x.now)
	if err == nil {
		t.Fatal("storage failure reported success")
	}
	x.onRead = nil
	x.db, err = store.Open(t.Context(), x.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s := x.state(t)
	if s.Complete || s.Cursor != 0 {
		t.Fatalf("storage failure advanced %+v", s)
	}
	if _, err := os.Stat(owner.WorktreePath); err != nil {
		t.Fatal("storage failure removed checkout")
	}
	if strings.Contains(x.logs.String(), `"msg":"retirement sweep complete"`) {
		t.Fatal("storage failure logged completed pass")
	}
	if !strings.Contains(x.logs.String(), `"msg":"retirement sweep incomplete"`) {
		t.Fatal("missing failure summary")
	}
}

func TestRetirementSweepRealRateLimitPreservesCursor(t *testing.T) {
	x := newSweepRealFixture(t)
	x.owner(t, 213)
	d := x.daemon()
	sweepSeed(d, x.now)
	x.status.Store(http.StatusTooManyRequests)
	if err := d.RunRetirementBatch(t.Context(), x.now); err == nil {
		t.Fatal("rate limit not reported")
	}
	if s := x.state(t); s.Cursor != 0 || s.Complete {
		t.Fatalf("rate-limited owner skipped %+v", s)
	}
	if _, held := d.backoff.hold(x.now); !held {
		t.Fatal("HTTP rate limit did not arm backoff")
	}
	calls := x.calls.Load()
	if err := d.RunRetirementBatch(t.Context(), x.now); err != nil {
		t.Fatal(err)
	}
	if x.calls.Load() != calls {
		t.Fatal("held sweep made HTTP request")
	}
}

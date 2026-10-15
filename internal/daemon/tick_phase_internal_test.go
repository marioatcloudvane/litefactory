package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type phaseProgressExecutor struct {
	turnExecutor
	errors []error
}

func (e *phaseProgressExecutor) PollStoryProgress(ctx context.Context) error {
	err := e.turnExecutor.PollStoryProgress(ctx)
	e.errors = append(e.errors, err)
	return err
}

func TestTickStoryHTTP2DeadlineAndRetry(t *testing.T) {
	f := newPublicationIO(t)
	var requests atomic.Int32
	cancelled := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sub_issues") {
			if r.ProtoMajor != 2 {
				t.Errorf("protocol = %s", r.Proto)
			}
			if r.Header.Get("Authorization") == "" {
				t.Error("missing SDK authentication")
			}
			if requests.Add(1) == 1 {
				w.WriteHeader(http.StatusOK)
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Error(err)
					return
				}
				select {
				case <-r.Context().Done():
					close(cancelled)
				case <-t.Context().Done():
				}
				return
			}
		}
		if n, err := strconv.Atoi(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]); err == nil {
			_, _ = fmt.Fprintf(w, `{"id":%d,"number":%d,"state":"open"}`, n*10, n)
			return
		}
		_, _ = fmt.Fprint(w, `[]`)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	hc := server.Client()
	hc.Timeout = 5 * time.Second
	client, err := github.New(github.Options{APIBase: server.URL, HTTPClient: hc, Token: secrets.NewToken("offline-token"), RequestContext: func(ctx context.Context) (context.Context, func(error), error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("provider request lacks phase deadline")
		}
		if ctx.Err() != nil {
			t.Error("independent provider request inherited expired context")
		}
		return ctx, func(error) {}, nil
	}})
	publicationMust(t, err)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit, Logger: f.d.log})
	publicationMust(t, err)
	progress, err := turn.NewStoryProgress(f.db, client, renderer, 20, t.TempDir())
	publicationMust(t, err)
	parent := f.session.TaskRef
	publicationMust(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	publicationMust(t, f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent, Generation: 1, State: "implementing"}, "planning"))
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "phase-test", store.AgentClaude)
	publicationMust(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "phase-test.log" }})
	publicationMust(t, err)
	publicationMust(t, progress.Register(t.Context(), &agent.ApprovedBundle{Parent: parent, Generation: 1, RunID: run.ID}, t.TempDir()))
	d := f.d
	executor := &phaseProgressExecutor{turnExecutor: turnExecutor{opts: turn.Options{DB: f.db, StoryProgress: progress}}}
	d.turn = executor
	d.forge = client
	d.schedule = newDiscoverySchedule(time.Minute)
	d.pool = newPool(1)
	d.repoSessionCursor = make(map[int64]int64)
	d.minFreeDiskBytes = 1
	d.diskPath = t.TempDir()
	d.freeDiskBytes = func(string) (uint64, error) { return 0, nil }
	d.tickPhaseTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err = d.Tick(t.Context())
	publicationMust(t, err)
	if elapsed := time.Since(start); elapsed > d.tickPhaseTimeout+2*time.Second {
		t.Fatalf("tick took %s", elapsed)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP/2 request was not cancelled")
	}
	if len(executor.errors) != 1 || !errors.Is(executor.errors[0], context.DeadlineExceeded) {
		t.Fatalf("first poll errors: %v", executor.errors)
	}
	if requests.Load() != 1 {
		t.Fatalf("first tick requests = %d", requests.Load())
	}
	retained, err := f.db.GetStoryProgressRun(t.Context(), run.ID)
	publicationMust(t, err)
	if !retained.Active {
		t.Fatal("failed poll consumed pending run")
	}
	_, err = d.Tick(t.Context())
	publicationMust(t, err)
	if len(executor.errors) != 2 || executor.errors[1] != nil {
		t.Fatalf("retry errors: %v", executor.errors)
	}
	if requests.Load() != 2 {
		t.Fatalf("retry requests = %d", requests.Load())
	}
	// The real progress mutex is available immediately: Tick left no detached poll.
	publicationMust(t, d.tickPhase(t.Context(), progress.Poll))
	if requests.Load() != 3 {
		t.Fatal("progress did not reconcile after retry")
	}
}

func TestTickPhaseContexts(t *testing.T) {
	d := &Daemon{tickPhaseTimeout: 10 * time.Millisecond}
	parent, stop := context.WithCancel(t.Context())
	defer stop()
	var first context.Context
	err := d.tickPhase(parent, func(ctx context.Context) error { first = ctx; <-ctx.Done(); return ctx.Err() })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	publicationMust(t, d.tickPhase(parent, func(ctx context.Context) error {
		if ctx.Err() != nil || first.Err() == nil {
			t.Fatal("phases share cancellation")
		}
		return nil
	}))
	if parent.Err() != nil {
		t.Fatal("phase cancelled daemon")
	}
	early, cancel := context.WithTimeout(parent, time.Millisecond)
	defer cancel()
	child, release := d.tickPhaseContext(early)
	defer release()
	want, _ := early.Deadline()
	got, _ := child.Deadline()
	if !want.Equal(got) {
		t.Fatal("caller deadline extended")
	}
}

func TestAdmissionPhasePreservesStoppedWorkerLifetime(t *testing.T) {
	d, work, _ := stoppedResultFixture(t)
	d.tickPhaseTimeout = time.Millisecond
	entered := make(chan context.Context, 1)
	repair := func(ctx context.Context) (turn.Result, error) {
		entered <- ctx
		<-ctx.Done()
		return turn.Result{}, ctx.Err()
	}
	d.runWorker(t.Context(), work, func() { d.retainStoppedResult(work, repair, turn.Trigger{CommentID: 12}, "") })
	parent, shutdown := context.WithCancel(t.Context())
	defer shutdown()
	if n := d.retryStoppedResults(parent, time.Now()); n != 1 {
		t.Fatalf("dispatched %d", n)
	}
	var worker context.Context
	select {
	case worker = <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	// Admission has already returned and cancelled its phase context.
	if worker.Err() != nil {
		t.Fatal("admission cancelled worker")
	}
	if _, bounded := worker.Deadline(); bounded {
		t.Fatal("worker inherited admission deadline")
	}
	shutdown()
	select {
	case <-worker.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel worker")
	}
	joinStoppedWorkers(t, d)
}

type stalledDiscoveryForge struct {
	Forge
	observed context.Context
}

func (f *stalledDiscoveryForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{Number: 1, State: "open"}, nil
}

func (f *stalledDiscoveryForge) ListIssues(ctx context.Context, _ github.Repo, _ time.Time) ([]github.Issue, error) {
	f.observed = ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestTickDiscoveryDeadlineRetainsCursor(t *testing.T) {
	f := newPublicationIO(t)
	client, err := github.New(github.Options{APIBase: f.server.URL, HTTPClient: f.server.Client(), Token: secrets.NewToken("offline-token")})
	publicationMust(t, err)
	forge := &stalledDiscoveryForge{Forge: client}
	d := f.d
	d.turn = turnExecutor{opts: turn.Options{DB: f.db}}
	d.forge = forge
	d.schedule = newDiscoverySchedule(time.Minute)
	d.pool = newPool(1)
	d.tickPhaseTimeout = 100 * time.Millisecond
	before, err := f.db.InitPollCursor(t.Context(), f.now.Add(-time.Minute))
	publicationMust(t, err)
	_, err = d.Tick(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("discovery result: %v", err)
	}
	if forge.observed == nil {
		t.Fatal("listing was not attempted")
	}
	if _, ok := forge.observed.Deadline(); !ok {
		t.Fatal("discovery lacks deadline")
	}
	after, err := f.db.GetPollCursor(t.Context())
	publicationMust(t, err)
	if before != after {
		t.Fatalf("cursor advanced: before=%+v after=%+v", before, after)
	}
}

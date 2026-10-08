package daemon

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/testutil"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type stoppedResultReporter struct {
	Reporter
	results []turn.Result
}

func (r *stoppedResultReporter) ReportTurn(_ context.Context, result turn.Result) (turn.Posted, error) {
	r.results = append(r.results, result)
	return turn.Posted{CommentID: 77}, nil
}

func stoppedResultFixture(t *testing.T) (*Daemon, *task, *stoppedResultReporter) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := &stoppedResultReporter{}
	// No store or executor: stopped-result repair must not depend on either.
	d := &Daemon{pool: newPool(1), reporter: reporter, log: log}
	work := &task{session: store.Session{ID: 42}, log: log, workerLog: log}
	if !d.inflight.enter(42) || !d.pool.try() {
		t.Fatal("initial admission failed")
	}
	return d, work, reporter
}

func joinStoppedWorkers(t *testing.T, d *Daemon) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cancel()
	if err := d.Quiesce(ctx); err != nil {
		t.Fatalf("repair worker did not finish: %v", err)
	}
}

func TestStoppedResultRepair_PreservesOwnershipUntilPersistence(t *testing.T) {
	d, work, reporter := stoppedResultFixture(t)
	var attempts int
	finished := turn.Result{Disposition: turn.DispositionRan, Run: store.Run{ID: 99, Status: store.RunFailed}}
	repair := func(context.Context) (turn.Result, error) {
		attempts++
		if attempts <= 2 {
			return turn.Result{}, errors.New("all storage unavailable")
		}
		return finished, nil
	}
	d.runWorker(t.Context(), work, func() { d.retainStoppedResult(work, repair, turn.Trigger{CommentID: 12}, "") })
	for attempt := 1; attempt <= 3; attempt++ {
		if got := d.retryStoppedResults(t.Context(), time.Date(2026, 9, 10, 1, []int{0, 1, 2, 4}[attempt], 0, 0, time.UTC)); got != 1 {
			t.Fatalf("attempt %d: repair did not reuse released capacity: %d", attempt, got)
		}
		joinStoppedWorkers(t, d)
		if attempts != attempt {
			t.Fatalf("attempts=%d, want %d", attempts, attempt)
		}
		if attempt <= 2 {
			if d.inflight.enter(42) {
				t.Fatal("failed persistence released task for relaunch")
			}
			if len(reporter.results) != 0 {
				t.Fatal("failed persistence reported an outcome")
			}
		}
	}
	if len(reporter.results) != 1 || reporter.results[0].Run.ID != 99 || reporter.results[0].Run.Status != store.RunFailed {
		t.Fatalf("reported result=%+v", reporter.results)
	}
	if d.inflight.size() != 0 {
		t.Fatal("successful repair retained task ownership")
	}
	if got := d.retryStoppedResults(t.Context(), time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)); got != 0 {
		t.Fatalf("completed debt dispatched again: %d", got)
	}
	if len(reporter.results) != 1 {
		t.Fatal("completed outcome reported twice")
	}
}

func TestStoppedResultRepair_WaitsForOwnedWorkerCleanup(t *testing.T) {
	d, work, reporter := stoppedResultFixture(t)
	ready, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int64
	go d.runWorker(t.Context(), work, func() {
		d.retainStoppedResult(work, func(context.Context) (turn.Result, error) {
			attempts.Add(1)
			return turn.Result{}, errors.New("not saved")
		}, turn.Trigger{}, "")
		close(ready)
		<-release
	})
	select {
	case <-ready:
	case <-time.After(testutil.Wait):
		t.Fatal("worker did not retain stopped evidence")
	}
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	got := d.retryStoppedResults(t.Context(), now)
	close(release)
	joinStoppedWorkers(t, d)
	if got != 0 || attempts.Load() != 0 {
		t.Fatal("repair overlapped original worker cleanup")
	}
	if len(reporter.results) != 0 || d.inflight.size() != 1 {
		t.Fatal("active ownership did not defer silently")
	}
	if got := d.retryStoppedResults(t.Context(), now.Add(time.Minute)); got != 1 {
		t.Fatalf("cleanup did not free capacity: %d", got)
	}
	joinStoppedWorkers(t, d)
	if attempts.Load() != 1 || d.inflight.size() != 1 {
		t.Fatal("failed repair lost ownership or did not run")
	}
}

func TestStoppedResultRepair_HoldsDoNotEraseEvidence(t *testing.T) {
	for _, name := range []string{"cancelled", "rate limited"} {
		t.Run(name, func(t *testing.T) {
			d, work, reporter := stoppedResultFixture(t)
			var attempts atomic.Int64
			d.runWorker(t.Context(), work, func() {
				d.retainStoppedResult(work, func(context.Context) (turn.Result, error) {
					attempts.Add(1)
					return turn.Result{}, errors.New("storage unavailable")
				}, turn.Trigger{}, "")
			})
			now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "cancelled" {
				cancel()
			} else {
				d.backoff.fail(now, time.Minute)
			}
			if got := d.retryStoppedResults(ctx, now); got != 0 {
				t.Fatalf("held repair dispatched=%d", got)
			}
			if attempts.Load() != 0 || len(reporter.results) != 0 || d.inflight.size() != 1 {
				t.Fatal("hold changed retained outcome or ownership")
			}
			if got := d.retryStoppedResults(t.Context(), now.Add(time.Hour)); got != 1 {
				t.Fatalf("retained repair not eligible after hold: %d", got)
			}
			joinStoppedWorkers(t, d)
			if attempts.Load() != 1 || d.inflight.size() != 1 {
				t.Fatal("repair evidence was lost")
			}
		})
	}
}

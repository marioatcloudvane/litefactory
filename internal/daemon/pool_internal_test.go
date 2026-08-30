package daemon

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
)

// TestPoolTryStopsAtCapacityWithoutBlocking is the bound and the non-blocking
// half of it in one assertion. The non-blocking half is what the test's own
// completion proves: a try that blocked on a full pool would hang here, which
// is exactly what it would do to discovery.
func TestPoolTryStopsAtCapacityWithoutBlocking(t *testing.T) {
	t.Parallel()

	for _, size := range []int{1, 3, MaxConcurrentRuns} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()

			p := newPool(size)
			for i := range size {
				if !p.try() {
					t.Fatalf("slot %d of %d was refused by an idle pool", i+1, size)
				}
			}
			if p.try() {
				t.Fatalf("a %d-slot pool handed out %d slots", size, size+1)
			}

			// A released slot is reusable — the pool is a bound on how many run
			// at once, not on how many ever run.
			p.done()
			if !p.try() {
				t.Error("a released slot was not handed out again")
			}
			if p.try() {
				t.Error("the pool went over capacity after a release")
			}

			for range size {
				p.done()
			}
			p.wait()
		})
	}
}

// TestPoolSizeIsFloored covers the one value that would be silently fatal: a
// pool of zero capacity is a daemon that looks alive and dispatches nothing.
// Options.validate refuses it long before here; this is the floor underneath.
func TestPoolSizeIsFloored(t *testing.T) {
	t.Parallel()

	for _, size := range []int{-1, 0} {
		p := newPool(size)
		if !p.try() {
			t.Errorf("a pool built with size %d cannot dispatch anything", size)
			continue
		}
		p.done()
	}
}

// TestPoolWaitJoinsEveryWorker is the shutdown join. wait must not return until
// the last worker has finished, because the whole difference between a run that
// completed and a run the next start has to report as abandoned is what happens
// in these few milliseconds.
func TestPoolWaitJoinsEveryWorker(t *testing.T) {
	t.Parallel()

	const size = 4
	p := newPool(size)
	release := make(chan struct{})
	var finished atomic.Int32

	for i := range size {
		if !p.try() {
			t.Fatalf("worker %d was refused a slot", i)
		}
		go func() {
			defer p.done()
			<-release
			finished.Add(1)
		}()
	}

	// Nothing has finished yet and the pool is full, so a dispatch now is
	// refused rather than queued.
	if p.try() {
		t.Error("a full pool handed out a slot")
	}

	close(release)
	p.wait()

	if got := int(finished.Load()); got != size {
		t.Errorf("wait returned with %d of %d workers finished", got, size)
	}
	// Every slot is free again by the time wait returns, so the next tick can
	// dispatch a full window.
	for i := range size {
		if !p.try() {
			t.Errorf("slot %d was still held after wait returned", i+1)
		}
	}
}

// TestPoolWaitOnAnEmptyPoolReturnsImmediately is the state every daemon that
// has never dispatched anything shuts down in — including, today, every daemon,
// because the dispatch call site does not exist yet. A wait that blocked here
// would burn the whole shutdown grace on nothing.
func TestPoolWaitOnAnEmptyPoolReturnsImmediately(t *testing.T) {
	t.Parallel()

	// Reaching the next line is the assertion: a wait that blocked would fail
	// this test by timing out.
	newPool(2).wait()
}

// TestPoolQuiesceWaitsForTheWorker covers the exported [Daemon.Quiesce] path:
// with a live context it is the same join as wait, and it returns nil.
func TestPoolQuiesceWaitsForTheWorker(t *testing.T) {
	t.Parallel()

	p := newPool(2)
	release := make(chan struct{})
	var finished atomic.Bool

	if !p.try() {
		t.Fatal("an idle pool refused a slot")
	}
	go func() {
		defer p.done()
		<-release
		finished.Store(true)
	}()

	close(release)
	if err := p.quiesce(t.Context()); err != nil {
		t.Fatalf("quiesce: %v", err)
	}
	if !finished.Load() {
		t.Error("quiesce returned while a worker was still running")
	}
	if err := p.quiesce(t.Context()); err != nil {
		t.Errorf("quiesce on an idle pool: %v", err)
	}
}

// TestPoolQuiesceGivesUpOnAContextThatIsDone is the difference between quiesce
// and wait, and the reason shutdown uses wait: a caller with a cancelled
// context gets its context error back and the worker keeps running. Doing that
// on the shutdown path would abandon every run instead of joining it.
func TestPoolQuiesceGivesUpOnAContextThatIsDone(t *testing.T) {
	t.Parallel()

	p := newPool(2)
	release := make(chan struct{})

	if !p.try() {
		t.Fatal("an idle pool refused a slot")
	}
	go func() {
		defer p.done()
		<-release
	}()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.quiesce(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("quiesce with a cancelled context = %v, want %v", err, context.Canceled)
	}

	close(release)
	p.wait()
}

// TestDaemonQuiesceOnAnIdleDaemon exercises the exported wrapper on the state
// the fixture's settle primitive will meet most often: a daemon whose tick
// dispatched nothing. It must come back at once and say nothing is wrong.
//
// The daemon is built by hand rather than by [New] because nothing here needs a
// database, a forge or a reporter — the pool and the in-flight set are the only
// state the answer depends on, and both are what [New] leaves empty.
func TestDaemonQuiesceOnAnIdleDaemon(t *testing.T) {
	t.Parallel()

	d := &Daemon{pool: newPool(DefaultConcurrentRuns)}
	if got := d.inflight.size(); got != 0 {
		t.Errorf("a daemon that has dispatched nothing reports %d sessions in flight", got)
	}
	if err := d.Quiesce(t.Context()); err != nil {
		t.Errorf("Quiesce on an idle daemon: %v", err)
	}
}

package daemon

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestInflightAdmitsOneOwnerPerSession is the whole point of the set: a second
// dispatch for a session already in flight must be told no, and told no by the
// same call that would have taken it, so that there is no window between asking
// and taking.
func TestInflightAdmitsOneOwnerPerSession(t *testing.T) {
	t.Parallel()

	var f inflight
	if !f.enter(7) {
		t.Fatal("the first enter for session 7 was refused")
	}
	if f.enter(7) {
		t.Error("session 7 was entered twice; two workers would be running one task")
	}
	if !f.enter(8) {
		t.Error("session 8 was refused while session 7 was in flight; sessions are independent")
	}

	f.leave(7)
	if !f.enter(7) {
		t.Error("session 7 was still held after leave")
	}
	if got := f.size(); got != 2 {
		t.Errorf("in flight = %d, want 2 (sessions 7 and 8)", got)
	}

	f.leave(7)
	f.leave(8)
	// Leaving what is not there is a no-op: leave is called from a worker's
	// defer, which must never be the thing that kills the daemon.
	f.leave(9)
	if got := f.size(); got != 0 {
		t.Errorf("in flight = %d after every session left, want 0", got)
	}
}

// TestInflightPeakIsTheObservedMaximum pins peak as a high-water mark rather
// than a running total or a current count: it goes up with simultaneity and
// never comes back down.
func TestInflightPeakIsTheObservedMaximum(t *testing.T) {
	t.Parallel()

	var f inflight
	if got := f.peak(); got != 0 {
		t.Errorf("a fresh set peaks at %d, want 0", got)
	}

	for _, session := range []int64{1, 2, 3} {
		if !f.enter(session) {
			t.Fatalf("session %d was refused", session)
		}
	}
	f.leave(1)
	f.leave(2)
	if !f.enter(4) {
		t.Fatal("session 4 was refused")
	}

	if got, want := f.peak(), 3; got != want {
		t.Errorf("peak = %d, want %d — three sessions were in flight at once", got, want)
	}
	if got, want := f.size(), 2; got != want {
		t.Errorf("in flight = %d, want %d", got, want)
	}
}

// TestInflightIsSafeForConcurrentUse runs the set the way the daemon will: many
// goroutines leaving while one keeps entering. The exclusivity assertion is the
// per-session holder count — if enter ever admitted two owners at once, some
// session would be seen held twice — and -race covers the rest.
func TestInflightIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	const (
		sessions = 8
		rounds   = 500
	)
	var (
		f       inflight
		held    [sessions]atomic.Int32
		doubled atomic.Bool
		wg      sync.WaitGroup
	)

	for worker := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range rounds {
				session := int64((worker + round) % sessions)
				if !f.enter(session) {
					continue
				}
				if held[session].Add(1) != 1 {
					doubled.Store(true)
				}
				held[session].Add(-1)
				f.leave(session)
			}
		}()
	}
	wg.Wait()

	if doubled.Load() {
		t.Error("a session was held by two callers at once; enter is not a check-and-insert")
	}
	if got := f.size(); got != 0 {
		t.Errorf("in flight = %d after every worker left, want 0", got)
	}
	if got := f.peak(); got < 1 || got > sessions {
		t.Errorf("peak = %d, want between 1 and %d", got, sessions)
	}
}

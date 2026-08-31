package turn

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
)

// TestExecuteRefusesASimultaneousSecondCallAtOneSession is AC-PAR-4.5: two
// [Execute] calls racing the same session must produce exactly one runs row
// and exactly one posted report, and the call that loses must consume
// nothing, post nothing and advance no watermark.
//
// # Why the race is not left to the scheduler
//
// The two calls are not simply fired off together and hoped about. The
// second is only started once the first is *known* to be holding the session
// lock and blocked mid-claude — which
// [TestExecuteAllocatesTheRunRowBeforeTheSubprocess] establishes happens
// after AcquireSessionLock and after the run row is allocated. That is what
// makes the second call's DispositionLocked a certainty rather than a coin
// flip on goroutine scheduling: the property under test does not depend on
// which of the two calls happens to reach the database first, only on what
// the loser did, and blocking the winner mid-flight is what guarantees there
// is a loser to observe on every run of this test.
//
// This is also why RD-3's comment says duplicate dispatch within one process
// is unreachable through the daemon's ordinary dispatch path — the in-flight
// set refuses a second worker before it ever calls Execute. Driving the race
// here, directly against Execute, is what the plan asks the test to do
// instead: the store lock is defence in depth, and this is what proves the
// depth is real.
func TestExecuteRefusesASimultaneousSecondCallAtOneSession(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once

	var f *fixture
	runner := newStubRunner(func(ctx context.Context, req harness.Request) (*harness.RunResult, error) {
		enterOnce.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	type outcome struct {
		result Result
		err    error
	}
	winner := make(chan outcome, 1)
	go func() {
		result, err := Execute(context.Background(), f.opts, f.request())
		winner <- outcome{result, err}
	}()

	select {
	case <-entered:
	case <-time.After(gateTimeout):
		close(release)
		t.Fatal("the first Execute call never reached the point of holding the lock")
	}

	// The second call, deliberately made while the first is still inside
	// claude and holding the session lock. It must be refused outright, not
	// queued and not retried here.
	loser := f.execute(f.request())
	close(release)

	var won outcome
	select {
	case won = <-winner:
	case <-time.After(gateTimeout):
		t.Fatal("the first Execute call never returned after being released")
	}
	if won.err != nil {
		t.Fatalf("the winning Execute returned an error: %v\nlogs:\n%s", won.err, f.logs)
	}

	if won.result.Disposition != DispositionRan {
		t.Errorf("winner disposition = %s, want %s", won.result.Disposition, DispositionRan)
	}
	if loser.Disposition != DispositionLocked {
		t.Fatalf("loser disposition = %s, want %s", loser.Disposition, DispositionLocked)
	}
	if loser.ConsumesComment() {
		t.Error("the loser's result claims to consume its comment; a contended lock must not")
	}
	if loser.Reportable() {
		t.Error("the loser's result claims to be reportable; a contended lock owes the issue nothing")
	}

	if runs := f.runs(); len(runs) != 1 {
		t.Fatalf("recorded %d runs rows for one session, want exactly 1", len(runs))
	}

	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	wonPosted, err := reporter.ReportTurn(t.Context(), won.result)
	if err != nil {
		t.Fatalf("ReportTurn on the winner: %v", err)
	}
	if wonPosted.Skipped {
		t.Error("the winner's report was skipped, want it posted")
	}
	if got := len(forge.all()); got != 1 {
		t.Fatalf("forge saw %d posts after the winner's report, want 1", got)
	}

	lostPosted, err := reporter.ReportTurn(t.Context(), loser)
	if err != nil {
		t.Fatalf("ReportTurn on the loser: %v", err)
	}
	if !lostPosted.Skipped {
		t.Error("the loser's report was not skipped; a locked turn must post nothing")
	}
	if got := len(forge.all()); got != 1 {
		t.Errorf("forge saw %d posts after the loser's report, want still 1: the loser must post nothing", got)
	}

	if wm := watermark(t, f); wm != testComment {
		t.Errorf("watermark = %d, want %d: only the winner's report may move it", wm, testComment)
	}
}

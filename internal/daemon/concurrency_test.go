package daemon_test

import (
	"context"
	"github.com/cloudvane/litefactory/internal/testutil"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

// rendezvousTimeout bounds every channel rendezvous in this package's
// concurrency tests. A bug here is a goroutine that never reaches the point
// it was supposed to, which is a deadlock; "panic: test timed out after 10m"
// names no test and no line, so every wait below fails on its own clock with
// a message that does (the house pattern from
// internal/turn/gate_test.go's gateTimeout).
const rendezvousTimeout = testutil.Wait

// TestTickReturnsWithoutWaitingForADispatchedRun is AC-PAR-1.1, the headline
// of the whole feature: a run blocked mid-Execute must not stop a later tick
// from discovering and dispatching a different issue.
//
// The first turn is held open on a channel rather than timed, so "the tick
// returned before the run finished" is provable rather than merely likely —
// if [daemon.Daemon.Tick] ever went back to blocking until its dispatched work
// completed, the second f.tick() below would deadlock behind the first turn's
// channel receive and this test would time out on its own clock instead of
// silently passing.
func TestTickReturnsWithoutWaitingForADispatchedRun(t *testing.T) {
	f := newFixture(t)

	const blockedComment = int64(201)
	entered := make(chan struct{})
	release := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == blockedComment {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return turn.Result{}, ctx.Err()
			}
		}
		return f.ranSuccessfully(ctx, req)
	})

	f.plantCursor()
	f.issue("Add a /health endpoint") // issue 123, alone
	f.settle()                        // bootstraps its watermark; nothing to dispatch yet

	f.forge.addComment(testIssue, blockedComment, owner(), "/spike block here")
	first := f.tick()
	if first.Dispatched != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("first tick dispatched %d turns, want 1\nlogs:\n%s", first.Dispatched, f.logs)
	}

	select {
	case <-entered:
	case <-time.After(rendezvousTimeout):
		close(release)
		t.Fatalf("the dispatched turn for comment %d never reached Execute within %v",
			blockedComment, rendezvousTimeout)
	}

	// Issue 123's session is now provably in flight: it has signalled entered
	// and has not been released. Calling Quiesce here would deadlock this test
	// on that very fact — which is exactly why it must not be called until
	// after release is closed, below. A second issue, registered and assigned
	// only now, is discovered and dispatched by a tick called while the first
	// is still blocked — which is only possible if Tick returned without
	// waiting for it. Authoring its body as the approver with a command that spawns a run
	// (AC-2.16b) makes its bootstrap itself produce a real Execute call, so
	// "a different issue is dispatched" is proven by a turn actually running,
	// not by a bootstrap that touched nothing.
	//
	// Issue 123 stays in the daemon's in-flight set for the whole of this
	// tick (RD-3/AC-PAR-5.3), so it is skipped rather than re-dispatched here,
	// which is what makes "1" below an exact count rather than a guess about
	// how fast the first worker happens to be.
	const secondIssue = 124
	f.forge.addIssue(fakeIssue{
		Number:    secondIssue,
		Title:     "A second task",
		Body:      "/spike do the other work",
		User:      owner(),
		Assignees: []fakeUser{owner()},
	})
	second := f.tick()
	if second.Dispatched != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick dispatched %d turns, want 1: a different issue must be discovered "+
			"and dispatched while the first is still blocked\nlogs:\n%s", second.Dispatched, f.logs)
	}

	close(release)
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	// The second issue's trigger is comment id 0 (an issue-body run, never a
	// comment — see [turn.Trigger.CommentID]), so [fakeExecutor.triggers]
	// reports it alongside the blocked comment's id, sorted.
	if got := f.turns.triggers(); !slices.Equal(got, []int64{0, blockedComment}) {
		t.Errorf("turns ran for %v, want [0 %d]", got, blockedComment)
	}
}

// TestTwoIssuesInOneRepoRunConcurrently is AC-PAR-1.2: both turns must be
// observably inside Execute at the same instant, not merely dispatched close
// together.
//
// Neither turn can proceed past the arrived signal until both have sent one,
// so reaching the assertions below is itself the proof of concurrency — a
// daemon that ran them one after another would leave the second one's send
// waiting forever behind the first one's still-open proceed receive, and the
// test would time out rather than pass. This is the same barrier
// internal/turn/gate_test.go's TestClaudeRunsOutsideTheCloneGate uses.
func TestTwoIssuesInOneRepoRunConcurrently(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 2})

	arrived := make(chan struct{}, 2)
	proceed := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		arrived <- struct{}{}
		select {
		case <-proceed:
		case <-ctx.Done():
			return turn.Result{}, ctx.Err()
		}
		return f.ranSuccessfully(ctx, req)
	})

	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addIssue(fakeIssue{Number: 124, Title: "A second task", User: stranger()})
	f.settle()

	f.forge.addComment(testIssue, 71, owner(), "/spike first")
	f.forge.addComment(124, 72, owner(), "/spike second")

	result := f.tick()
	if result.Dispatched != 2 {
		close(proceed)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("dispatched %d turns, want 2\nlogs:\n%s", result.Dispatched, f.logs)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(rendezvousTimeout):
			close(proceed)
			_ = f.daemon.Quiesce(f.t.Context())
			t.Fatalf("only %d of 2 turns reached Execute within %v; they are not running concurrently",
				i, rendezvousTimeout)
		}
	}
	close(proceed)

	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{71, 72}) {
		t.Errorf("turns ran for %v, want [71 72]", got)
	}
}

// AC-PAR-1.10 (the daemon singleton is unchanged) needs no new test here.
// TestSecondInstanceIsRefused in lock_test.go already exercises
// [daemon.AcquireLock] directly, and this feature makes no change anywhere
// near it — the singleton is a file lock taken once at startup, entirely
// outside Tick's discovery/dispatch path that this feature rewrote.

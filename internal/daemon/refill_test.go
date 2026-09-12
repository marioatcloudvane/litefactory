package daemon_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

// refillTicks bounds how many real ticks this test retries, after freeing one
// slot, waiting for a held-back turn to be dispatched into it.
//
// There is no event that means "a slot just freed". The forge post that ends
// a worker's turn happens well before its slot is actually released: after
// the HTTP handler returns there is still the rest of [Daemon.drain]'s own
// watermark read, [Daemon.clearBacklog], [Daemon.work] returning, and only
// then the worker's two deferred releases in [Daemon.offer] — inflight.leave
// first, pool.done() last, by construction (see the comment on the `go func`
// literal there). That is two more SQLite operations and two defers,
// contending with this test's own next Tick call over the one connection
// internal/store allows a database (SetMaxOpenConns(1)) — which is exactly
// why waiting for a fixed instant after a post is a coin flip and not a
// signal.
//
// What is available instead is the daemon's own mechanism: dispatch has no
// queue by design (RD-1), so a freed slot is taken up by whichever tick
// happens to run after it frees, not by the instant it frees. A bounded
// retry over real ticks — the daemon's own primitive, never a sleep and never
// a wall-clock poll — is the faithful expression of that.
const refillTicks = 20

// TestFourthStartsWhenFirstFinishesNotAfterAllThreeComplete is AC-PAR-1.6: 5
// comments on 5 different issues with max_concurrent_runs: 3 start exactly 3
// at once; the 4th starts when the first of those three finishes, and the
// 5th when the second finishes — never only once all three have completed.
//
// Which three of the five the round-robin actually starts first is not
// pinned down here — the rotation cursor is left over from bootstrapping five
// sessions two-at-a-time under a cap of three (below), and asserting a
// specific comment id would make the test fragile against exactly how that
// bootstrap happened to interleave. What is asserted is the *shape*: three
// start, two are held back, and the two held back start one at a time as the
// first two of the original three finish — identified at runtime via the
// started channel, not assumed by position.
//
// # Why this genuinely fails on the wrong behaviour
//
// The two comments this test keeps running throughout the middle of the test
// have their release channels closed only at the very end, well after both
// previously-held-back comments have already been confirmed started. If
// refill only happened once every one of the original three had completed —
// the specific wrong behaviour this criterion names — releasing just the
// first of the three could never produce a 4th start: the other two are
// never released either, so a batch-drain implementation could not complete
// its batch, and the retry loop below would exhaust refillTicks and fail with
// a named message instead of quietly passing. The explicit still-running
// check on the other two, right after the 4th is confirmed, closes the same
// gap for an implementation that batches in some other, less obvious shape.
func TestFourthStartsWhenFirstFinishesNotAfterAllThreeComplete(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 3})

	issues := [5]int{301, 302, 303, 304, 305}
	comments := [5]int64{9001, 9002, 9003, 9004, 9005}

	var release [5]chan struct{}
	for i := range release {
		release[i] = make(chan struct{})
	}
	// started reports the index (into issues/comments) of every turn as it
	// reaches Execute — a channel rendezvous for "which of the five just
	// started", read at runtime rather than assumed from rotation order.
	started := make(chan int, 5)
	commentIndex := make(map[int64]int, 5)
	for i, c := range comments {
		commentIndex[c] = i
	}
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		i, ok := commentIndex[req.Trigger.CommentID]
		if !ok {
			return f.ranSuccessfully(ctx, req)
		}
		started <- i
		select {
		case <-release[i]:
		case <-ctx.Done():
			return turn.Result{}, ctx.Err()
		}
		return f.ranSuccessfully(ctx, req)
	})

	releaseAll := func() {
		for _, ch := range release {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
	}

	// stillRunning reports whether a held turn's release channel has not been
	// closed yet — a non-blocking receive against a channel nothing ever sends
	// on, so the only way it "receives" is that the channel is already closed.
	// It is how this test asserts, rather than assumes, that a turn it has not
	// released is still blocked inside Execute at the instant being checked.
	stillRunning := func(i int) bool {
		select {
		case <-release[i]:
			return false
		default:
			return true
		}
	}

	f.plantCursor()
	for _, num := range issues {
		f.forge.addIssue(fakeIssue{Number: num, Title: fmt.Sprintf("Task %d", num), User: stranger()})
	}
	f.bootstrapIssues(f.repo, issues[:]...)
	for i, num := range issues {
		f.forge.addComment(num, comments[i], owner(), fmt.Sprintf("/spike task %d", num))
	}

	// Discovery reservations may serialize initial histories. Continue ticks
	// without releasing executions until all three pool slots are active.
	running := make(map[int]bool, 3)
	for attempts := 0; attempts < refillTicks && len(running) < 3; attempts++ {
		f.clock.advance(testInterval)
		f.tick()
	drain:
		for {
			select {
			case i := <-started:
				running[i] = true
			default:
				break drain
			}
		}
	}
	if len(running) != 3 {
		releaseAll()
		t.Fatalf("3 turns started but only %d distinct comments among them: %v", len(running), running)
	}
	var runningOrder []int
	for i := range 5 {
		if running[i] {
			runningOrder = append(runningOrder, i)
		}
	}
	heldBack := make(map[int]bool, 2)
	for i := range 5 {
		if !running[i] {
			heldBack[i] = true
		}
	}
	if len(heldBack) != 2 {
		releaseAll()
		t.Fatalf("want exactly 2 comments held back, got %d: %v", len(heldBack), heldBack)
	}

	// Release the first of the three running turns. The other two — whichever
	// they are — stay running: their release channels are not closed until
	// the very end. Refill happens on a subsequent real tick, not at the
	// instant the slot frees (see [refillTicks]), so this retries a bounded
	// number of ticks rather than assuming the very next one will see it.
	close(release[runningOrder[0]])
	fourth := -1
	for range refillTicks {
		f.clock.advance(testInterval)
		f.tick()
		select {
		case fourth = <-started:
		default:
		}
		if fourth >= 0 {
			break
		}
	}
	if fourth < 0 {
		releaseAll()
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("no held-back turn started within %d ticks of releasing the 1st running turn",
			refillTicks)
	}
	if !heldBack[fourth] {
		releaseAll()
		t.Fatalf("comment %d started, but it was one of the original three, not one of the two "+
			"held back (%v)", comments[fourth], heldBack)
	}
	// The teeth: the other two original turns must still be running when the
	// 4th starts. A batch-drain implementation that could only refill once
	// the whole original batch finished would need both of these released
	// too, which they are not.
	if !stillRunning(runningOrder[1]) || !stillRunning(runningOrder[2]) {
		releaseAll()
		t.Fatalf("comment %d started only once another of the original three turns had also "+
			"finished; want it to start while both are still running", comments[fourth])
	}
	delete(heldBack, fourth)

	// Release the second of the three original turns. The remaining
	// held-back comment must start the same way, while the third original
	// turn is still running.
	close(release[runningOrder[1]])
	fifth := -1
	for range refillTicks {
		f.clock.advance(testInterval)
		f.tick()
		select {
		case fifth = <-started:
		default:
		}
		if fifth >= 0 {
			break
		}
	}
	if fifth < 0 {
		releaseAll()
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("no held-back turn started within %d ticks of releasing the 2nd running turn",
			refillTicks)
	}
	if !heldBack[fifth] {
		releaseAll()
		t.Fatalf("comment %d started, want the one remaining held-back comment", comments[fifth])
	}
	if !stillRunning(runningOrder[2]) {
		releaseAll()
		t.Fatalf("comment %d started only after the 3rd original turn had also finished; want it "+
			"to start while that turn is still running", comments[fifth])
	}

	releaseAll()
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, comments[:]) {
		t.Errorf("turns ran for %v, want %v", got, comments)
	}
}

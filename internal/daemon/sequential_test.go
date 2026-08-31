package daemon_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestTwoCommentsOnOneIssueRunSequentiallyInOrder is AC-PAR-1.4: two comments
// on one issue run one after the other, in ascending comment id order, and
// the second never starts before the first's report has posted.
//
// The second comment arrives while the first is provably still busy — mid
// Execute, on a blocked worker — and a tick called at that exact moment is
// asserted to dispatch nothing at all: the in-flight set (RD-3) is what makes
// "the second never starts before the first's report has posted" true by
// construction rather than by timing luck. Only once the first is released
// and its worker has fully finished does a later tick pick the second one up.
func TestTwoCommentsOnOneIssueRunSequentiallyInOrder(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	const c1, c2 = int64(601), int64(602)
	entered := make(chan struct{})
	release := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == c1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return turn.Result{}, ctx.Err()
			}
		}
		return f.ranSuccessfully(ctx, req)
	})

	f.forge.addComment(testIssue, c1, owner(), "/spike first")
	first := f.tick()
	if first.Dispatched != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("first tick dispatched %d, want 1\nlogs:\n%s", first.Dispatched, f.logs)
	}
	select {
	case <-entered:
	case <-time.After(rendezvousTimeout):
		close(release)
		t.Fatalf("comment %d's turn never reached Execute within %v", c1, rendezvousTimeout)
	}

	// The second comment arrives while the session is still busy on the
	// first. A tick called right now must dispatch nothing for this session —
	// it is already in flight — so there is no way for the second comment's
	// turn to start before the first's report has posted.
	f.forge.addComment(testIssue, c2, owner(), "/spike second")
	mid := f.tick()
	if mid.Dispatched != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("mid tick dispatched %d turns, want 0: the session is already in flight on comment "+
			"%d\nlogs:\n%s", mid.Dispatched, c1, f.logs)
	}

	close(release)
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}

	// The first comment's worker only ever knew about comment 1 (its own
	// ListCommentsAfter ran before comment 2 existed), so it finished cleanly
	// on its own — comment 2 is discovered fresh, on a later tick.
	last := f.settle()
	if last.Dispatched != 1 {
		t.Fatalf("last tick dispatched %d turns, want 1: comment %d must still be found\nlogs:\n%s",
			last.Dispatched, c2, f.logs)
	}

	session := f.session()
	if got := f.turns.triggersFor(session.ID); !slices.Equal(got, []int64{c1, c2}) {
		t.Errorf("turns ran for session %d in order %v, want [%d %d]", session.ID, got, c1, c2)
	}

	// The externally visible order, within this one issue: react to comment
	// 1, run its turn, post its report — then, and only then, the same three
	// steps for comment 2.
	order := f.forge.orderFor(testIssue)
	if len(order) != 6 {
		t.Fatalf("order = %v, want 6 entries (react/turn/post twice)", order)
	}
	wantPrefixes := []string{"react:", "turn:", "post:", "react:", "turn:", "post:"}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(order[i], want) {
			t.Errorf("order[%d] = %q, want prefix %q\nfull order: %v", i, order[i], want, order)
		}
	}
	if order[1] != fmt.Sprintf("turn:%d", c1) {
		t.Errorf("order[1] = %q, want turn:%d — comment 1's turn must run before comment 2's report",
			order[1], c1)
	}
	if order[4] != fmt.Sprintf("turn:%d", c2) {
		t.Errorf("order[4] = %q, want turn:%d — comment 2's turn must run only after comment 1's "+
			"report (order[2]) has posted", order[4], c2)
	}
	if got := f.watermark(); got != c2 {
		t.Errorf("watermark = %d, want %d", got, c2)
	}
}

// TestFiveCommentsOnOneIssueOccupyExactlyOneSlot is AC-PAR-1.5: 5 comments on
// one issue with max_concurrent_runs: 3 run one at a time in order, occupying
// exactly one slot, and two slots remain available to other issues for the
// whole sequence.
//
// The dispatch unit is the session, not the comment (D-2/RD-1's design), so a
// session's whole comment queue is one [pool.try] and one [pool.done] no
// matter how many comments it holds. Two other issues are dispatched
// concurrently while the 5-comment session is blocked on its very first
// comment — reaching the barrier below is itself the proof that two slots
// were free the entire time the third was occupied by all five comments.
func TestFiveCommentsOnOneIssueOccupyExactlyOneSlot(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 3})

	const c1, otherA, otherB = int64(701), int64(711), int64(721)
	queue := []int64{701, 702, 703, 704, 705}

	entered := make(chan struct{})
	release := make(chan struct{})
	arrived := make(chan struct{}, 2)
	proceed := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		switch req.Trigger.CommentID {
		case c1:
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return turn.Result{}, ctx.Err()
			}
		case otherA, otherB:
			arrived <- struct{}{}
			select {
			case <-proceed:
			case <-ctx.Done():
				return turn.Result{}, ctx.Err()
			}
		}
		return f.ranSuccessfully(ctx, req)
	})

	f.plantCursor()
	f.issue("Add a /health endpoint") // 123, the 5-comment issue
	f.forge.addIssue(fakeIssue{Number: 124, Title: "Other task A", User: stranger()})
	f.forge.addIssue(fakeIssue{Number: 125, Title: "Other task B", User: stranger()})
	f.settle()

	// Push all three issues out of the listing window before touching any of
	// them again. Left inside it, issues 124 and 125 would be re-listed and
	// given their own harmless no-op dispatch on *every* tick below — a race
	// against the ones this test cares about, since nothing here waits for a
	// non-blocked worker to finish. Out of the window, a tick only rediscovers
	// one of them once it has something new to say, exactly once.
	f.clock.advance(pastListing)
	f.settle()

	for _, id := range queue {
		f.forge.addComment(testIssue, id, owner(), fmt.Sprintf("/spike comment %d", id))
	}
	first := f.tick()
	if first.Dispatched != 1 {
		close(release)
		close(proceed)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("first tick dispatched %d turns, want 1\nlogs:\n%s", first.Dispatched, f.logs)
	}
	select {
	case <-entered:
	case <-time.After(rendezvousTimeout):
		close(release)
		close(proceed)
		t.Fatalf("comment %d's turn never reached Execute within %v", c1, rendezvousTimeout)
	}

	// The 5-comment session holds one slot. Two other issues, dispatched now,
	// must both be able to reach Execute at once — proving two slots are free
	// regardless of how many comments the busy session has queued.
	f.forge.addComment(124, otherA, owner(), "/spike other A")
	f.forge.addComment(125, otherB, owner(), "/spike other B")
	second := f.tick()
	if second.Dispatched != 2 {
		close(release)
		close(proceed)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick dispatched %d turns, want 2: two slots must be free while the "+
			"5-comment session holds only one\nlogs:\n%s", second.Dispatched, f.logs)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(rendezvousTimeout):
			close(release)
			close(proceed)
			_ = f.daemon.Quiesce(f.t.Context())
			t.Fatalf("only %d of 2 other turns reached Execute within %v; two slots were not "+
				"actually free", i, rendezvousTimeout)
		}
	}
	close(proceed)
	close(release)

	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}

	session := f.session()
	if got := f.turns.triggersFor(session.ID); !slices.Equal(got, queue) {
		t.Errorf("the 5-comment session's turns ran in order %v, want %v", got, queue)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{701, 702, 703, 704, 705, otherA, otherB}) {
		t.Errorf("turns ran for %v, want the 5-comment session's queue plus [%d %d]", got, otherA, otherB)
	}
}

// TestAQueuedCommentBehindABusySessionOccupiesNoSlot is AC-PAR-1.7: with a
// cap of 1 and issue A busy, issue B's eligible comment is not blocked by
// issue A's *queued* second comment.
//
// The dispatch unit is the session: issue A's queued second comment adds
// nothing to what the pool is tracking (still exactly one occupied slot), so
// once issue A's single worker finishes, the freed slot goes to whichever
// candidate the fairness rotation visits first — which, by construction
// (issue B's repository is the cyclic successor of the one that dispatched
// last), is issue B, not issue A's own backlog.
func TestAQueuedCommentBehindABusySessionOccupiesNoSlot(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
	other := f.secondRepo()
	const otherIssue = 200
	const busyComment = int64(801)

	entered := make(chan struct{})
	release := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == busyComment {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return turn.Result{}, ctx.Err()
			}
		}
		return f.ranSuccessfully(ctx, req)
	})

	// Bootstrap both sessions before either has a comment. The cap is 1, so
	// this deliberately takes two ticks: fixed and bounded, not a retry loop.
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	f.forge.addIssueIn(other.Owner, other.Name, fakeIssue{Number: otherIssue, Title: "Issue B", User: stranger()})
	f.settle()

	f.forge.addComment(testIssue, busyComment, owner(), "/spike busy")
	first := f.tick()
	if first.Dispatched != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("first tick dispatched %d, want 1\nlogs:\n%s", first.Dispatched, f.logs)
	}
	select {
	case <-entered:
	case <-time.After(rendezvousTimeout):
		close(release)
		t.Fatalf("comment %d's turn never reached Execute within %v", busyComment, rendezvousTimeout)
	}

	// Issue A's session is busy on the first comment, and a second queues up
	// behind it. Issue B has its own, unrelated eligible comment.
	f.forge.addComment(testIssue, 802, owner(), "/spike queued behind the busy session")
	f.forge.addCommentTo(other.Owner, other.Name, otherIssue, 803, owner(), "/spike issue B")
	second := f.tick()
	if second.Dispatched != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick dispatched %d turns, want 0: the daemon's only slot is held by "+
			"issue A\nlogs:\n%s", second.Dispatched, f.logs)
	}

	close(release)
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}

	// The slot is free now. Issue B's repository is the cyclic successor of
	// the one that dispatched last (issue A's), so it is visited — and given
	// a free slot — first: issue B's comment is what claims it, deterministically.
	third := f.tick()
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if third.Dispatched < 1 {
		t.Fatalf("third tick dispatched %d turns, want at least 1\nlogs:\n%s", third.Dispatched, f.logs)
	}
	if got := f.turns.triggers(); !slices.Contains(got, int64(803)) {
		t.Errorf("turns ran for %v; issue B's comment (803) must have claimed the freed slot, not "+
			"be starved behind issue A's queued second comment\nlogs:\n%s", got, f.logs)
	}

	// Whatever is left over — issue A's queued comment, if this tick's single
	// slot went to issue B alone — runs on the next, final tick.
	_ = f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{busyComment, 802, 803}) {
		t.Errorf("turns ran for %v, want [%d 802 803]\nlogs:\n%s", got, busyComment, f.logs)
	}
}

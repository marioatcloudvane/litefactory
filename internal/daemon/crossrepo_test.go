package daemon_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/turn"
)

// TestTwoIssuesInTwoReposRunConcurrently is AC-PAR-1.3: the same barrier proof
// as AC-PAR-1.2, across two registered repositories instead of two issues in
// one. Neither turn can proceed past its arrived signal until both have sent
// one, so reaching the assertions below is itself the proof of concurrency.
func TestTwoIssuesInTwoReposRunConcurrently(t *testing.T) {
	f := newFixture(t)
	other := f.secondRepo()
	const otherIssue = 200

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
	f.forge.addIssueIn(other.Owner, other.Name, fakeIssue{Number: otherIssue, Title: "Repo B's task", User: stranger()})
	f.bootstrapIssues(f.repo, testIssue)
	f.bootstrapIssues(other, otherIssue)

	// Admit each new command on subsequent ticks while the first execution
	// stays blocked; the barrier still proves real overlap across repositories.
	for i := range 2 {
		if i == 0 {
			f.forge.addComment(testIssue, 81, owner(), "/spike in repo A")
		} else {
			f.forge.addCommentTo(other.Owner, other.Name, otherIssue, 82, owner(), "/spike in repo B")
		}
		started := false
		for range refillTicks {
			f.clock.advance(testInterval)
			f.tick()
			select {
			case <-arrived:
				started = true
			default:
			}
			if started {
				break
			}
		}
		if !started {
			close(proceed)
			_ = f.daemon.Quiesce(f.t.Context())
			t.Fatalf("only %d of 2 turns reached Execute through admitted ticks\n%s", i, f.logs)
		}
	}
	close(proceed)

	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{81, 82}) {
		t.Errorf("turns ran for %v, want [81 82]", got)
	}
}

// TestInFlightIssueInOneRepoDoesNotHoldBackDiscoveryOfAnother is AC-PAR-5.1.
//
// Repo A's session is dispatched and blocked mid-Execute, indefinitely. The
// pool is left at its default size, deliberately: what this test isolates is
// the in-flight case of AC-PAR-5.1 (a busy session, not a busy *pool*), so
// repo B always has a free slot of its own and its progress is attributable
// only to repo A's session being in flight — not to any contention over
// capacity, which is a different mechanism (RD-1, exercised in fairness_test.go).
func TestInFlightIssueInOneRepoDoesNotHoldBackDiscoveryOfAnother(t *testing.T) {
	f := newFixture(t)
	other := f.secondRepo()
	const otherIssue = 200
	const stuckComment = int64(301)

	blocked := make(chan struct{})
	release := make(chan struct{})
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == stuckComment {
			close(blocked)
			select {
			case <-release:
			case <-ctx.Done():
				return turn.Result{}, ctx.Err()
			}
		}
		return f.ranSuccessfully(ctx, req)
	})

	// Both sessions are bootstrapped together, before either has a comment:
	// this is what plants repo B's watermark at 0 so that its real comment,
	// added later, is seen as new work rather than history a first sight
	// would silently skip (AC-2.10).
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addIssueIn(other.Owner, other.Name, fakeIssue{Number: otherIssue, Title: "Repo B's task", User: stranger()})
	f.settle()

	// Push both issues out of the listing window before touching either
	// again. Left inside it, repo B would be re-listed and given its own
	// harmless no-op dispatch on the very next tick too — a race against the
	// dispatch this test cares about, since nothing here waits for that
	// non-blocked worker to finish before checking the tick after it. Out of
	// the window, a tick only rediscovers a session once it has something new
	// to say, exactly once.
	f.clock.advance(pastListing)
	f.settle()

	f.forge.addComment(testIssue, stuckComment, owner(), "/spike stuck in repo A")
	first := f.tick()
	if first.Dispatched != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("first tick dispatched %d turns, want 1\nlogs:\n%s", first.Dispatched, f.logs)
	}
	select {
	case <-blocked:
	case <-time.After(rendezvousTimeout):
		close(release)
		t.Fatalf("the stuck turn never reached Execute within %v\nlogs:\n%s", rendezvousTimeout, f.logs)
	}

	// Repo A's session is now provably in flight, indefinitely — it has not
	// been released. Repo B's comment must still be discovered and
	// dispatched by a tick called while repo A is stuck, and the poll cursor
	// must still advance to that tick's own start.
	f.forge.addCommentTo(other.Owner, other.Name, otherIssue, 302, owner(), "/spike in repo B")
	second := f.tick()
	if second.Dispatched != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick dispatched %d turns, want 1: repo B's comment must be dispatched "+
			"while repo A's session is in flight\nlogs:\n%s", second.Dispatched, f.logs)
	}
	if !second.Cursor.TasksUpdatedSince.Equal(second.StartedAt) {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("cursor advanced to %s, want %s: repo A's stuck session must not rewind or hold "+
			"back the cursor for repo B (RD-2)", second.Cursor.TasksUpdatedSince, second.StartedAt)
	}

	close(release)
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{stuckComment, 302}) {
		t.Errorf("turns ran for %v, want [%d 302]\nlogs:\n%s", got, stuckComment, f.logs)
	}
}

// TestABusySessionIsRediscoveredAfterListingOverlapWithoutAnUpdatedAtChange is
// AC-PAR-5.2: an issue busy — in flight, provably, on a blocked worker — for
// longer than [daemon.ListingOverlap] must still be found and correctly
// finished once it is free, even though the listing can no longer see it,
// because nothing about the issue ever changed.
func TestABusySessionIsRediscoveredAfterListingOverlapWithoutAnUpdatedAtChange(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	const busyComment = int64(401)
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

	f.forge.addComment(testIssue, busyComment, owner(), "/spike while busy")
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
		t.Fatal("the busy turn never reached Execute")
	}

	// More than ListingOverlap passes while the session is still busy — in
	// flight, provably, since it has not been released. Nothing about the
	// issue changes in that time: no new comment, no relabel, nothing — its
	// UpdatedAt is exactly what it was when the comment first arrived.
	f.clock.advance(pastListing)

	// One tick is needed to carry the advanced clock into the stored cursor:
	// the listing window a tick uses is anchored to the *previous* tick's own
	// start time, not to the clock at the instant the tick runs. This is the
	// discriminating assertion for that: if the window were instead recomputed
	// from the clock at the instant this tick runs, the advance above would
	// already have pushed it past the issue's unchanged UpdatedAt, and this
	// listing would see nothing — so seeing the issue here is proof the window
	// is still the one anchored to the *stored* cursor, one tick stale. Either
	// way its session is correctly skipped, because it is still in flight.
	carryOver := f.tick()
	if carryOver.Issues != 1 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("carry-over tick's listing saw %d issues, want 1: the window must still be the "+
			"one anchored to the stored cursor from before the clock advance, not one recomputed "+
			"from the clock at the instant this tick runs", carryOver.Issues)
	}
	if carryOver.Dispatched != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("carry-over tick dispatched %d turns, want 0: the session is still in flight",
			carryOver.Dispatched)
	}

	// Now the window has genuinely moved past the issue's unchanged
	// UpdatedAt. The session must still be recognised as busy — found
	// through the backlog, not lost through a listing that cannot see it —
	// and its comment is exactly what runs once it is released.
	second := f.tick()
	if second.Issues != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick's listing saw %d issues, want 0: the window has moved past this "+
			"issue's unchanged UpdatedAt, so a listing cannot be what still tracks it", second.Issues)
	}
	if second.Dispatched != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick dispatched %d turns, want 0: the busy session is still in flight and "+
			"must be skipped, not redispatched", second.Dispatched)
	}

	close(release)
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{busyComment}) {
		t.Errorf("turns ran for %v, want [%d]", got, busyComment)
	}
	if got := f.watermark(); got != busyComment {
		t.Errorf("watermark = %d, want %d: the busy session's run finished and was reported",
			got, busyComment)
	}
	if session := f.sessionOf(testIssue); !session.NeedsPoll {
		t.Error("needs_poll is clear before the one-shot mode label has been refreshed")
	}

	// And, for completeness: once free, discovery finds nothing left to do —
	// the session was never lost, and nothing runs it a second time.
	final := f.settle()
	if final.Dispatched != 1 {
		t.Errorf("final tick dispatched %d turns, want 1 backlog refresh", final.Dispatched)
	}
	if again := f.settle(); again.Dispatched != 0 {
		t.Errorf("post-refresh tick dispatched %d turns", again.Dispatched)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{busyComment}) {
		t.Errorf("turns ran for %v after the final tick, want still [%d]: no double-processing",
			got, busyComment)
	}
}

// TestDiscoverySkipsASessionAlreadyInFlight is AC-PAR-5.3: a session this
// daemon is already running a turn for must be skipped by discovery, posting
// nothing and moving no watermark, and the skip must be logged with the
// task's identity.
func TestDiscoverySkipsASessionAlreadyInFlight(t *testing.T) {
	f := newFixture(t)

	const busyComment = int64(501)
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

	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, busyComment, owner(), "/spike stay busy")
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
		t.Fatal("the busy turn never reached Execute")
	}

	// A second comment on the same issue, recent enough that the listing
	// finds the issue again — but the session is already in flight, and
	// discovery must skip it rather than redispatch it.
	f.forge.addComment(testIssue, 502, owner(), "/spike while busy")
	second := f.tick()

	if second.Dispatched != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Fatalf("second tick dispatched %d turns, want 0: the session is already in flight",
			second.Dispatched)
	}
	if second.Deferred != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Errorf("second tick deferred %d sessions, want 0: a session already in flight is "+
			"skipped, not deferred (skipping costs no slot)", second.Deferred)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Errorf("posted %d comments before the busy turn even finished, want 0", len(posted))
	}
	wantTask := "task=github:" + testOwnerName + "/" + testRepoName + "#" + strconv.Itoa(testIssue)
	if !strings.Contains(f.logs.String(), "already has a turn in flight") ||
		!strings.Contains(f.logs.String(), wantTask) {
		close(release)
		_ = f.daemon.Quiesce(f.t.Context())
		t.Errorf("the skip was not logged with task identity (want %q)\nlogs:\n%s", wantTask, f.logs)
	}

	close(release)
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}

	// Only the first comment was consumed: the busy worker's own comment list
	// was fetched before the second comment existed, so the second is still
	// unconsumed and posted nothing yet either.
	if got := f.watermark(); got != busyComment {
		t.Errorf("watermark = %d, want %d: the skipped tick must not have moved it", got, busyComment)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Errorf("posted %d comments, want 1 (only the first comment's report)", len(posted))
	}

	// And nothing was lost: a later tick finds and drains the second comment
	// normally.
	third := f.settle()
	if third.Dispatched != 1 {
		t.Errorf("third tick dispatched %d turns, want 1: the second comment must still be found",
			third.Dispatched)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{busyComment, 502}) {
		t.Errorf("turns ran for %v, want [%d 502]", got, busyComment)
	}
}

package daemon_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The backlog flag is sessions.needs_poll, the durable re-discovery axis a
// concurrent tick needs (RD-2). These tests pin the write protocol; the read
// side is now live — the poll cursor is monotonic and [daemon.Daemon.discover]
// unions the forge listings with the flagged sessions, so this flag is what
// brings a tick back to work it did not finish.
//
// The property under test is the asymmetry, and only the asymmetry. A flag left
// standing on a session that turned out to have no work is self-healing: a
// later tick re-lists it, finds nothing above its watermark and clears it. A
// flag cleared on a session that *did* still have work is silence on the
// thread, which AC-4.1 forbids by name. So every abnormal exit must leave it
// set unless an authoritative closed-issue observation retires ordinary retry
// intent. A fully drained, error-free turn clears ordinary open backlog.

// TestACleanDrainClearsTheBacklogFlag covers ordinary open backlog clearing: a
// comment that ran, reported and was consumed, with nothing left behind.
func TestACleanDrainClearsTheBacklogFlag(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 201, owner(), "/spike add the endpoint")
	f.settle()
	f.settle() // refresh after consuming the one-shot mode label

	if got := f.watermark(); got != 201 {
		t.Fatalf("watermark is %d, want 201: the comment must actually have been consumed", got)
	}
	if session := f.session(); session.NeedsPoll {
		t.Errorf("needs_poll is set after a drain that reached the end of the queue; "+
			"the clean path must clear it\nlogs:\n%s", f.logs)
	}
}

// TestACleanBootstrapClearsTheBacklogFlag is the same rule on the first-sight
// path: the watermark was planted, no comment was owed, nothing is left.
func TestACleanBootstrapClearsTheBacklogFlag(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")

	result := f.settle()

	if result.Dispatched != 1 {
		t.Fatalf("dispatched %d tasks, want 1", result.Dispatched)
	}
	if got := f.watermark(); got != 0 {
		t.Fatalf("watermark is %d, want 0: the first sight must have planted one", got)
	}
	if session := f.session(); session.NeedsPoll {
		t.Errorf("needs_poll is set after a clean bootstrap\nlogs:\n%s", f.logs)
	}
}

// TestAFailedBootstrapLeavesTheBacklogFlagSet is the evidence that the flag is
// set at the dispatch point rather than after the work: the worker never
// reaches the end of the bootstrap, and the session still carries the flag.
//
// The tick itself succeeds now. Dispatch severed the return-value chain that
// used to carry a worker's failure up into Tick's error branch, and that is the
// design: with two turns in flight, one task's refused comment listing is not a
// reason to end the other's tick. What the failure leaves behind is this flag
// and a logged line, which is what the next tick reads.
func TestAFailedBootstrapLeavesTheBacklogFlagSet(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")

	// The bootstrap's own listing of the existing comments is the first
	// per-issue comment listing of the run, and it is refused.
	f.forge.onListComments = func(w http.ResponseWriter, call int) bool {
		if call != 1 {
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}

	f.settle()

	if got := f.watermark(); got != -1 {
		t.Fatalf("watermark is %d, want -1: nothing was planted", got)
	}
	if session := f.session(); !session.NeedsPoll {
		t.Errorf("needs_poll is clear after a bootstrap that never finished; the flag is set "+
			"before the work so that no abnormal exit can lose the session\nlogs:\n%s", f.logs)
	}
}

// TestAnUnconsumedCommentLeavesTheBacklogFlagSet is the drain stopping early:
// comment 211's report was refused, so the queue stops with it unconsumed and
// the session is still owed a look.
//
// The second half is the self-healing direction: once the retry lands, the same
// mechanism clears the flag with no compensating write anywhere.
func TestAnUnconsumedCommentLeavesTheBacklogFlagSet(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 211, owner(), "/spike first")
	f.forge.addComment(testIssue, 212, owner(), "/spike second")

	// The report for comment 211 is refused, which is what leaves it unconsumed.
	f.forge.onPostComment = func(w http.ResponseWriter, call int) bool {
		if call != 1 {
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}

	f.settle()

	if got := f.watermark(); got >= 211 {
		t.Fatalf("watermark is %d, want below 211: the drain must have stopped on it\nlogs:\n%s",
			got, f.logs)
	}
	if session := f.session(); !session.NeedsPoll {
		t.Fatalf("needs_poll is clear after a drain that stopped on an unconsumed comment\n"+
			"logs:\n%s", f.logs)
	}

	f.forge.onPostComment = nil
	f.settle()
	f.settle()
	f.settle() // consume the second instruction, then prove the backlog self-heals

	if got := f.watermark(); got != 212 {
		t.Fatalf("watermark is %d after the retry, want 212", got)
	}
	if session := f.session(); session.NeedsPoll {
		t.Errorf("needs_poll survived the retry that drained the queue; a stale-true flag "+
			"must be self-healing\nlogs:\n%s", f.logs)
	}
}

// TestALockedTaskLeavesTheBacklogFlagSet is RD-4: DispositionLocked is promoted
// from a drop to a marker-set. Another process holds the task's lock, so
// nothing ran, nothing was consumed, and the session must be reconsidered.
func TestALockedTaskLeavesTheBacklogFlagSet(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 221, owner(), "/spike while the task is busy")
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		return f.turnResult(ctx, req, turn.DispositionLocked, harness.OutcomeSucceeded)
	})

	bootstrapped := f.watermark()
	f.settle()

	if got := f.watermark(); got != bootstrapped {
		t.Fatalf("watermark is %d, want %d: a contended lock consumes nothing", got, bootstrapped)
	}
	if session := f.session(); !session.NeedsPoll {
		t.Errorf("needs_poll is clear after a turn that found the task locked\nlogs:\n%s", f.logs)
	}
}

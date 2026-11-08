package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/turn"
)

// One owner comment that fails every turn no longer blocks the task: after
// three failed turns it is answered with a plain "not used" reply naming the
// error, consumed, and the comment behind it runs.
func TestFailingOwnerCommentIsSetAsideAfterThreeTurns(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 211, owner(), "/spike first")
	f.forge.addComment(testIssue, 212, owner(), "/spike second")
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == 211 {
			return turn.Result{}, errors.New("worktree preparation exploded")
		}
		return f.ranSuccessfully(ctx, req)
	})

	for range 8 {
		f.clock.advance(time.Hour)
		f.settle()
		if slices.Contains(f.turns.triggers(), 212) {
			break
		}
	}
	failed := 0
	for _, id := range f.turns.triggers() {
		if id == 211 {
			failed++
		}
	}
	if failed != 3 {
		t.Fatalf("comment 211 ran %d times, want 3 before it is set aside\nlogs:\n%s", failed, f.logs)
	}
	if !slices.Contains(f.turns.triggers(), 212) {
		t.Fatalf("the comment behind the failing one never ran\nlogs:\n%s", f.logs)
	}
	posted, _, _ := f.forge.snapshot()
	notUsed := 0
	for _, body := range posted {
		if strings.Contains(body, "Not used") && strings.Contains(body, "worktree preparation exploded") {
			notUsed++
		}
	}
	if notUsed != 1 {
		t.Fatalf("not-used replies naming the error = %d, want 1: %v", notUsed, posted)
	}
}

// A report whose post keeps failing moves to the background: after three failed
// posts the comment is consumed, the next comment runs, and the owed post is
// still delivered once the forge accepts it.
func TestFailingReportPostDoesNotHoldLaterComments(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 211, owner(), "/spike first")
	f.forge.addComment(testIssue, 212, owner(), "/spike second")
	refusing := true
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		if !refusing {
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}
	for range 10 {
		f.clock.advance(time.Hour)
		f.settle()
		if slices.Contains(f.turns.triggers(), 212) {
			break
		}
	}
	if !slices.Contains(f.turns.triggers(), 212) {
		t.Fatalf("a failing report post held the next comment for ever: triggers=%v\nlogs:\n%s", f.turns.triggers(), f.logs)
	}
	if got := f.watermark(); got < 211 {
		t.Fatalf("watermark=%d, want past 211", got)
	}
	if session := f.session(); !session.NeedsPoll {
		t.Fatal("owed report left no backlog flag to bring the task back")
	}
	refusing = false
	for range 4 {
		f.clock.advance(time.Hour)
		f.settle()
	}
	if pending, err := f.db.PendingCommentReportTriggers(t.Context(), f.session().ID, 1<<40); err != nil || slices.Contains(pending, 211) {
		t.Fatalf("owed report for 211 never delivered: %v %v\nlogs:\n%s", pending, err, f.logs)
	}
}

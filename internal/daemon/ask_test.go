package daemon_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/turn"
)

// TestATagOnlyCommentStartsNoRunAndIsConsumed is AC-A1.9, exercised through the
// whole daemon dispatch rather than at the parser alone.
//
// internal/turn's own tests pin that a comment containing only [turn.AskTag]
// classifies as [turn.KindNone] and that ParseCommand never returns an error
// for it (F-16) — but nothing before this test walked that classification
// through [daemon.Daemon]'s comment handling to confirm the daemon actually
// does what KindNone means for this specific shape of body: no run started, no
// reply drawn, and the comment still consumed so the next tick does not read it
// again. TestRunlessCommandsReplyAndAdvanceTheWatermark covers the same
// dispatch branch for a body that is merely whitespace; a tag-only body is a
// different, non-empty input that reaches the same branch only because
// ParseCommand strips the tag first, and that is the part worth pinning here.
//
// Before F-16 was identified and fixed, a tag-only comment fell through as free
// text with an empty instruction and the run died on ErrInvalidPrompt — a
// failed run that named nothing the owner could act on. A regression here would
// show up as a run or a reply where this test expects neither.
func TestATagOnlyCommentStartsNoRunAndIsConsumed(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight

	f.forge.addComment(testIssue, 90, owner(), turn.AskTag)

	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns for a tag-only comment, want none", len(calls))
	}
	if got := f.watermark(); got != 90 {
		t.Errorf("watermark is %d, want 90 — the comment must still be consumed", got)
	}
	if posted, reacted, _ := f.forge.snapshot(); len(posted) != 0 || len(reacted) != 0 {
		t.Errorf("posted %d comments and %d reactions for a tag-only comment, want none",
			len(posted), len(reacted))
	}
}

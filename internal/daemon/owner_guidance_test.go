package daemon_test

import (
	"github.com/cloudvane/litefactory/internal/store"
	"strings"
	"testing"
)

func TestOwnerGuidanceCommandsStaySeparateFromRunsAndPendingInstructions(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Optional standing guidance")
	f.settle()
	for _, c := range []struct {
		id         int64
		body, want string
	}{
		{40, "/guide Use plain language.", "Use plain language."},
		{50, "/guide", "Use plain language."},
		{60, "/guide Use ISO dates.", "Use ISO dates."},
		{70, "/guide clear", "Standing owner guidance is cleared."},
		{80, "/guide", "Standing owner guidance is cleared."},
	} {
		before, _, _ := f.forge.snapshot()
		f.forge.addComment(testIssue, c.id, owner(), c.body)
		f.settle()
		after, _, _ := f.forge.snapshot()
		if len(after) != len(before)+1 || !strings.Contains(after[len(after)-1], c.want) {
			t.Fatalf("guide %q response=%v logs=%s", c.body, after, f.logs)
		}
		if f.watermark() < c.id {
			t.Fatalf("guide response did not consume confirmed input %d", c.id)
		}
	}
	if len(f.turns.calls()) != 0 {
		t.Fatalf("guide dispatched agent work: %v", f.turns.calls())
	}
	session := f.sessionOf(testIssue)
	pending, err := f.db.ListOwnerInstructions(t.Context(), session.ID, 0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("guide entered ordinary instruction queue: %+v %v", pending, err)
	}
	current, err := f.db.CurrentOwnerGuidance(t.Context(), session.ID)
	if err != nil || current.Revision == 0 || current.Body != "" {
		t.Fatalf("clear not retained: %+v %v", current, err)
	}
}

func TestOwnerInputEditedReplayKeepsAdmittedBytesAndMakesProgress(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "crash before input retention", true: "crash after input retention"}[retained], func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issue("Replay admitted owner input")
			f.settle()
			session := f.sessionOf(testIssue)
			original := "  Keep the original explanation.  "
			if _, err := f.db.RecordComment(t.Context(), store.Comment{SessionID: session.ID, ForgeCommentID: 40, AuthorUserID: ownerID, AuthorHandle: ownerLogin, Body: original, CreatedAt: f.clock.Now(), Classification: store.ClassificationInstruction}); err != nil {
				t.Fatal(err)
			}
			if retained {
				if _, err := f.db.RetainOwnerInstruction(t.Context(), session.ID, 40, ownerID, original, store.OwnerInputTarget{}); err != nil {
					t.Fatal(err)
				}
			}
			f.forge.addComment(testIssue, 40, owner(), "/validate-ship -- edited source must not authorize shipping")
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Trigger.Body != original {
				t.Fatalf("edited replay changed routing/input: %+v logs=%s", calls, f.logs)
			}
			input, err := f.db.GetOwnerInstruction(t.Context(), session.ID, 40)
			if err != nil || input.Body != original {
				t.Fatalf("admitted input changed: %+v %v", input, err)
			}
			if f.watermark() < 40 {
				t.Fatalf("edited replay stalled ingestion: %d logs=%s", f.watermark(), f.logs)
			}
		})
	}
}

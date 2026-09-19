package daemon_test

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestResearchGraphDaemonConsumesWithoutWholeTurnReport(t *testing.T) {
	f := newFixture(t)
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.AuthorID != ownerID {
			t.Errorf("lost numeric owner: %d", req.Trigger.AuthorID)
		}
		result, err := f.ranSuccessfully(ctx, req)
		result.ResearchGraph = true
		return result, err
	}
	f.plantCursor()
	f.issue("Inspect graph boundaries")
	f.settle()
	f.forge.addComment(testIssue, 40, owner(), "/research inspect boundaries")
	first := f.settle()
	if first.Dispatched != 1 || f.watermark() != 40 {
		t.Fatalf("graph input not consumed after handling: %+v watermark=%d", first, f.watermark())
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 0 {
		t.Fatalf("daemon posted unconditional whole-turn report: %v", posted)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunSucceeded || runs[0].ResultCommentID != 0 {
		t.Fatalf("input receipt became publication: %+v", runs)
	}
	f.forge.addComment(testIssue, 41, owner(), "continue research")
	second := f.settle()
	if second.Dispatched != 1 || len(f.turns.calls()) != 2 || f.watermark() != 41 {
		t.Fatalf("graph return retained worker: %+v calls=%d watermark=%d", second, len(f.turns.calls()), f.watermark())
	}
}

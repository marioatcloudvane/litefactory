package daemon_test

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

type deliveryExecutor struct {
	*fakeExecutor
	mu       sync.Mutex
	controls []turn.DeliveryControl
}

func (e *deliveryExecutor) ApplyDelivery(_ context.Context, c turn.DeliveryControl) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.controls = append(e.controls, c)
	return nil
}
func TestDeliveryControlsStayOwnerOnlyAndNeverDispatchAgents(t *testing.T) {
	f := newFixture(t)
	exec := &deliveryExecutor{fakeExecutor: f.turns}
	f.daemon = f.newDaemon(daemon.Options{Turn: exec})
	f.plantCursor()
	f.issue("Delivery task")
	f.settle()
	f.forge.addComment(testIssue, 501, stranger(), "/deliver 1")
	f.forge.addComment(testIssue, 502, owner(), "/deliver 0")
	f.forge.addComment(testIssue, 503, owner(), "/merge bad")
	f.forge.addComment(testIssue, 504, owner(), "/deliver 1")
	f.settle()
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.controls) != 3 || exec.controls[2].CommentID != 504 || f.watermark() != 504 || len(f.turns.calls()) != 0 {
		t.Fatalf("routing or consumption incorrect: controls=%+v watermark=%d agentcalls=%d", exec.controls, f.watermark(), len(f.turns.calls()))
	}
	for _, c := range exec.controls {
		if c.ActorID != ownerID || c.OwnerID != ownerID {
			t.Fatalf("numeric authority lost %+v", c)
		}
	}
}

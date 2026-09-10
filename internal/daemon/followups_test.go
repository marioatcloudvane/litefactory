package daemon_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

type followupExecutor struct {
	*fakeExecutor
	mu       sync.Mutex
	controls []turn.FollowupControl
}

func (e *followupExecutor) ApplyFollowup(_ context.Context, c turn.FollowupControl) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.controls = append(e.controls, c)
	return nil
}
func TestFollowupOwnerControlsConsumeWithoutAgentDispatch(t *testing.T) {
	f := newFixture(t)
	exec := &followupExecutor{fakeExecutor: f.turns}
	f.daemon = f.newDaemon(daemon.Options{Turn: exec})
	f.plantCursor()
	f.issue("Follow-up task")
	f.settle()
	f.forge.addComment(testIssue, 501, stranger(), "/followup 1 "+strings.Repeat("a", 64)+" attach")
	f.forge.addComment(testIssue, 502, owner(), "/followup malformed")
	f.forge.addComment(testIssue, 503, owner(), "/followup 1 "+strings.Repeat("a", 64)+" linked")
	f.settle()
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.controls) != 2 || exec.controls[0].CommentID != 502 || exec.controls[1].CommentID != 503 {
		t.Fatalf("owner control routing %+v", exec.controls)
	}
	for _, c := range exec.controls {
		if c.ActorID != ownerID || c.OwnerID != ownerID {
			t.Fatalf("numeric authority lost %+v", c)
		}
	}
	if f.watermark() != 503 || len(f.turns.calls()) != 0 {
		t.Fatalf("control dispatch blocked queue or ran agent: watermark=%d calls=%d", f.watermark(), len(f.turns.calls()))
	}
}

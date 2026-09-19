package daemon

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// The daemon cannot call a turn's own validation, so the rules it has to
// satisfy are asserted here instead. A live dispatch failed every tick with
// "free_text has no instruction text" because nothing covered this seam: the
// selection was tested, the workflow was tested, and the request between them
// was not.
func TestGraphStepRequestIsAcceptableToATurn(t *testing.T) {
	t.Parallel()
	dispatch := store.GraphDispatch{ID: 7, Node: "shape"}
	trigger, command := graphStepRequest(dispatch, "owner")

	if !command.Spawns() {
		t.Fatalf("a graph step must spawn a run, got kind %s", command.Kind)
	}
	if strings.TrimSpace(command.Text) == "" {
		t.Fatal("a turn refuses a free-text command with no instruction text")
	}
	if strings.TrimSpace(trigger.Body) == "" {
		t.Fatal("the trigger body is what the turn records as the request")
	}
	if trigger.CommentID != dispatch.TriggerID() {
		t.Fatalf("trigger %d is not the dispatch's own identity %d", trigger.CommentID, dispatch.TriggerID())
	}
	if !store.IsGraphTrigger(trigger.CommentID) {
		t.Fatal("a graph step must run under a graph trigger, or it consumes the comment watermark")
	}
	if !strings.Contains(command.Text, dispatch.Node) {
		t.Errorf("the instruction does not say which stage asked for the run: %q", command.Text)
	}
}

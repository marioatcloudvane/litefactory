package daemon_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A Validate instance's owed step runs in Validate even though its issue still
// carries the Implement label and its managed feature is implementing; it must
// never be taken by the Implement runtime.
func TestValidateGraphStepRunsInValidateWhateverTheLabelSays(t *testing.T) {
	f := newFixture(t)
	var modes []agent.Mode
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		out, err := f.ranSuccessfully(ctx, req)
		if err != nil {
			return out, err
		}
		if _, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok {
			modes = append(modes, req.Mode)
		}
		out.ResearchGraph = true
		return out, nil
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	ref := session.TaskRef
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: ref, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: ref, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate/validate.dot", Digest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "repair_agent", StartedAt: due, NextDueAt: due})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ParkGraphInstance(t.Context(), instance.ID, "repair_agent", due, due); err != nil {
		t.Fatal(err)
	}

	f.settle()

	if len(modes) != 1 || modes[0] != agent.ModeValidate {
		t.Fatalf("graph step modes = %v, want one validate step\nlogs:\n%s", modes, f.logs)
	}
}

package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// Replay cannot dispatch a profile whose workflow differs from the immutable
// definition, even when its digest and all other fields have been copied.
func TestBugWorkflowBoundaryPinnedDefinitionMismatch(t *testing.T) {
	for _, kind := range []string{"bug-profile-mode-pin", "mode-profile-bug-pin", "conflicting-profile-binding"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, nil)
			handler := &graphTestHandler{results: map[string][]ActivityResult{"work": {{Outcome: "done", Kind: store.GraphOutputText, Output: []byte("must not run")}}}}
			req := f.graphRequest(graphTestDOT, handler)
			if kind == "mode-profile-bug-pin" {
				// A distinct saved Bug definition is legal storage; supplying an old mode
				// profile for its instance must fail before an attempt is reserved.
				definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: req.Profile.Team, Workflow: "bug", ProfileVersion: req.Profile.ProfileVersion, Path: "test/bug.dot", Digest: req.Profile.Digest, CreatedAt: req.At})
				if err != nil {
					t.Fatal(err)
				}
				instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: req.Profile.Start, StartedAt: req.At})
				if err != nil {
					t.Fatal(err)
				}
				req.InstanceID = instance.ID
			} else {
				req.Profile.Workflow = agent.WorkflowBug
				if kind == "bug-profile-mode-pin" {
					req.Profile.Mode = ""
				}
			}
			before, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ExecuteNodeAction(t.Context(), f.opts, req)
			if err == nil || !strings.Contains(err.Error(), "pinned definition workflow") {
				t.Fatalf("mismatch was not refused at workflow boundary: %+v %v", result, err)
			}
			if len(handler.calls) != 0 || result.AttemptID != 0 {
				t.Fatalf("mismatch executed handler or reserved attempt: calls=%d result=%+v", len(handler.calls), result)
			}
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
			if err != nil || len(attempts) != 0 {
				t.Fatalf("mismatch persisted attempts: %+v %v", attempts, err)
			}
			after, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
			if err != nil || after.Status != before.Status || after.CurrentNode != before.CurrentNode || after.DefinitionID != before.DefinitionID {
				t.Fatalf("mismatch changed saved graph: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

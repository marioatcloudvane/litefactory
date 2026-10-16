package turn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// Cancellation after an uncertain publication must retain the proposed commit
// without advancing the graph to its dependent agent.
func TestCanceledSyncDispatchRetainsPublicationEvidence(t *testing.T) {
	f := newFixture(t, nil)
	req := f.graphRequest(graphTestDOT, nil)
	for i := range req.Profile.Nodes {
		if req.Profile.Nodes[i].Name == "work" {
			req.Profile.Nodes[i].Action = graphexec.SyncBaseAction
		}
	}
	req.StopOnFailure = true
	evidence := syncBaseOutput{Working: "working", Base: "main", Result: "error", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Publication: "uncertain", Reason: "publication response was lost"}
	raw, err := json.Marshal(evidence)
	mustProgress(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler := &graphTestHandler{results: map[string][]ActivityResult{"work": {{Kind: store.GraphOutputJSON, Output: raw}}}, errs: map[string]error{"work": context.Canceled}, hook: func(context.Context, Options, ActivityRequest) { cancel() }}
	mustProgress(t, req.Activities.Register(graphexec.SyncBaseAction, handler))
	result, err := ExecuteNodeAction(ctx, f.opts, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch error=%v", err)
	}
	attempt, err := f.db.GetGraphNodeAttempt(t.Context(), result.AttemptID)
	mustProgress(t, err)
	instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	mustProgress(t, err)
	if attempt.Status != store.GraphFailed || instance.Status != store.GraphFailed || instance.CurrentNode != "work" {
		t.Fatalf("failure did not block handoff: attempt=%+v instance=%+v", attempt, instance)
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
	mustProgress(t, err)
	retained := false
	for _, output := range outputs {
		if output.Kind == store.GraphOutputJSON {
			var out syncBaseOutput
			mustProgress(t, json.Unmarshal(output.Payload, &out))
			if out.Commit == evidence.Commit && out.Publication == "uncertain" {
				retained = true
			}
		}
	}
	if !retained {
		t.Fatal("cancellation lost proposed publication commit")
	}
	transitions, err := f.db.ListGraphTransitions(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(transitions) != 0 {
		t.Fatal("canceled sync advanced dependent graph node")
	}
}

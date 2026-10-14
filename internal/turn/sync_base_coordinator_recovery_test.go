package turn

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestCoordinatorConflictRecoveryKeepsResolverHandoffWithoutTaskCheckout(t *testing.T) {
	f := newFixture(t, nil)
	req := f.graphRequest(graphTestDOT, nil)
	for i := range req.Profile.Nodes {
		if req.Profile.Nodes[i].Name == "work" {
			req.Profile.Nodes[i].Action = graphexec.SyncBaseAction
		}
	}
	instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", StartedAt: req.At})
	mustProgress(t, err)
	mustProgress(t, f.db.FinishGraphNodeAttempt(t.Context(), attempt.ID, store.GraphSucceeded, "conflict", req.At))
	bound := false
	binding := SyncBaseBinding{Bind: func(_ context.Context, _ Options, a ActivityRequest) (SyncBaseTarget, error) {
		bound = true
		if a.NodeAttemptID != attempt.ID || a.InstanceID != instance.ID {
			t.Fatalf("wrong coordinator attempt: %+v", a)
		}
		return SyncBaseTarget{Working: "integration", Base: "main", Lock: func(context.Context) (func() error, bool, error) {
			t.Fatal("coordinator recovery acquired a retained-checkout lock")
			return nil, false, nil
		}}, nil
	}}
	mustProgress(t, verifySyncHandoffBeforeAgent(t.Context(), f.opts, req.Profile, instance, binding))
	if !bound {
		t.Fatal("coordinator trusted binding was not resolved")
	}
}

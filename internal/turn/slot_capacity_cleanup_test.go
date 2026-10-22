package turn

import (
	"context"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestSlotCapacityStaysReservedThroughWorkflowFinalization(t *testing.T) {
	pool := &slotStoryPool{total: 1}
	parent := &slotStoryParent{pool: pool}
	running := false
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		used, _ := pool.Capacity()
		if used != 1 {
			t.Fatalf("agent running with live cost %d", used)
		}
		running = true
		submitLiveResult(t, req, researchDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchDoneJSON, FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledResearchFixture(t, runner)
	finalized := false
	f.opts.ResearchDriver = func(ctx context.Context, opts Options, req Request, result Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (Result, error) {
		policy := ResearchWorkflowPolicy()
		handlers := WorkflowHandlers{}
		if policy.Handlers != nil {
			handlers = *policy.Handlers
		}
		scope := func(_ context.Context, _ store.GraphNodeAttempt) (agentresult.Context, error) {
			return policy.baseContext(true), nil
		}
		handlers.Control = ControlActivity{Profile: definition.Profile, Context: scope, Policy: policy}
		handlers.Comment = CommentActivity{Profile: definition.Profile, Reporter: opts.ResearchReporter, Context: scope, Policy: policy}
		handlers.Prepare = func(ctx context.Context, w *researchWorkflow, a ActivityRequest) (ResearchInvocation, error) {
			original := w.policy.Handlers
			w.policy.Handlers = nil
			defer func() { w.policy.Handlers = original }()
			return w.invocation(ctx, opts, a)
		}
		before := handlers.BeforeSave
		handlers.BeforeSave = func(ctx context.Context, w *researchWorkflow) error {
			used, _ := pool.Capacity()
			if !running || used != 1 {
				t.Fatalf("finalization lost executing reservation: ran=%v live=%d", running, used)
			}
			finalized = true
			if before != nil {
				return before(ctx, w)
			}
			return nil
		}
		policy.Handlers = &handlers
		return ExecuteWorkflow(ctx, opts, req, result, definition, admission, policy)
	}
	out, err := Execute(WithStepCapacity(t.Context(), parent), f.opts, req)
	if err != nil || !out.Succeeded() {
		t.Fatalf("workflow result %+v err=%v", out, err)
	}
	if !finalized {
		t.Fatal("workflow finalization did not run")
	}
	used, _ := pool.Capacity()
	if used != 0 {
		t.Fatalf("workflow returned with %d reserved slots", used)
	}
}

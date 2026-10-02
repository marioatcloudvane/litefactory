package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugRolePermissions are selected by the factory under its operator ceiling.
// They are never derived from issue labels, DOT attributes or agent output.
type BugRolePermissions struct {
	Tools, AllowedTools []string
	Sandbox             string
}

func (r *BugRuntime) prepareRole(ctx context.Context, w *researchWorkflow, a ActivityRequest, scope agentresult.Context, retained string) (ResearchInvocation, error) {
	if w.prep == nil || w.prep.run.ID != a.RunID || scope.Bug == nil {
		return ResearchInvocation{}, errors.New("bug native invocation has no prepared run or bound role")
	}
	req := w.prep.request
	req.Mode, req.Workflow = "", agent.WorkflowBug
	req.Invocation = agent.InvocationResearchNode
	req.Graph = &agent.GraphInvocation{InstanceID: a.InstanceID, AttemptID: a.NodeAttemptID, RunID: a.RunID, Node: a.Node}
	expected := *scope.Bug
	resolver := func(check context.Context) (agentresult.Context, error) {
		var current agentresult.Context
		var err error
		if expected.Role == "bug-analyst" {
			current, _, err = BugAnalystContext(check, r.opts.DB, w.req.SessionID, expected.Invocation)
		} else {
			current, _, err = BugFixerContext(check, r.opts.DB, w.req.SessionID, expected.Reservation)
		}
		if err == nil && (current.Bug == nil || *current.Bug != expected) {
			err = errors.New("bug invocation scope changed")
		}
		return current, err
	}
	route, err := w.openResultRoute(ctx, &req, &resultOverride{Resolve: resolver, Targets: agentresult.EditTargets("bug"), Pass: fmt.Sprintf("bug:%s:%s", expected.Role, expected.Invocation)})
	if err != nil {
		return ResearchInvocation{}, err
	}
	return w.finishRoute(ctx, route, &req, retained, w.prep.promptInput.SessionArtifactDir)
}

// finishBugAgent settles the real process and factory save before any record,
// child or delivery node consumes its result. Parking does not spend a budget.
func (r *BugRuntime) finishBugAgent(ctx context.Context, w *researchWorkflow, instance store.GraphInstance) (Result, error) {
	spent, _ := store.GraphTriggerDispatch(w.req.Trigger.CommentID)
	_, err := r.opts.DB.ParkGraphInstanceAfter(context.WithoutCancel(ctx), instance.ID, instance.CurrentNode, r.opts.MCP.Now(), r.opts.MCP.Now(), spent)
	return w.finish(ctx, err)
}

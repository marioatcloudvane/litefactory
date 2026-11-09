package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// GranteeAuthority is the access-gate decision that let a non-owner's input
// run. Only daemon code holding an allowed gate outcome sets it.
type GranteeAuthority struct {
	Actor      ticket.Identity
	Handle     string
	DecisionID int64
}

// workflowSpecBuilders are the factory registries that admit each workflow.
var workflowSpecBuilders = map[agent.Workflow]func() (graphexec.Registry, error){
	agent.WorkflowResearch:  ResearchSpecs,
	agent.WorkflowPlan:      PlanSpecs,
	agent.WorkflowImplement: ImplementSpecs,
	agent.WorkflowValidate:  ValidateFlowSpecs,
	agent.WorkflowSpike:     SpikeSpecs,
	agent.WorkflowDoctor:    DoctorSpecs,
	agent.WorkflowBug:       BugSpecs,
}

// SpecsForWorkflow returns the factory registry that admits workflow.
func SpecsForWorkflow(workflow agent.Workflow) (graphexec.Registry, error) {
	build, ok := workflowSpecBuilders[workflow]
	if !ok {
		return graphexec.Registry{}, fmt.Errorf("turn: unknown workflow %q", workflow)
	}
	return build()
}

// granteeText is the text the gate digested for this trigger: the comment or
// batch text, or the issue body for an issue-body trigger.
func granteeText(req Request) string {
	if req.Trigger.CommentID == 0 {
		return req.Issue.Body
	}
	return req.Trigger.Body
}

// verifyGranteeAuthority admits a grantee trigger only against its own final
// allow decision: same project, session, source, actor and text.
func verifyGranteeAuthority(ctx context.Context, opts Options, session store.Session, req Request) error {
	g := req.Trigger.Grantee
	if g == nil || g.DecisionID <= 0 {
		return store.ErrApproverUnauthorized
	}
	d, err := opts.DB.GetAccessDecision(ctx, g.DecisionID)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrApproverUnauthorized
	}
	if err != nil {
		return err
	}
	if d.State != store.AccessDecisionFinal || d.Decision != store.AccessAllow ||
		d.RepoID != session.RepoID || d.SessionID != req.SessionID ||
		!d.Source.Equal(req.Trigger.Source) ||
		!d.Actor.Equal(req.Trigger.Actor) || !d.Actor.Equal(g.Actor) ||
		d.RequestDigest != access.Digest(granteeText(req)) {
		return store.ErrApproverUnauthorized
	}
	return nil
}

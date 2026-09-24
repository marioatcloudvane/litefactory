package turn

import (
	"context"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// WorkflowPolicy supplies domain contracts to the shared, admitted mechanisms.
// It is factory-owned and frozen before dispatch; graph content cannot set it.
type WorkflowPolicy struct {
	// Handlers supplies a mode's own invocation preparation and node activities.
	// It supplies no result format: every mode's application result travels the
	// shared result route, judged by Decode, and Handlers only reads it back.
	// Nil retains Research's generic preparation and activities.
	Handlers *WorkflowHandlers

	// Retain encodes the output the graph keeps for later nodes, from the retained
	// accepted result. Nil keeps the checked node itself. Whatever it encodes is
	// derived from the retained result, never from the terminal envelope.
	Retain func(binding store.ResultBinding, revision int64, node agentresult.ResearchNode) ([]byte, error)

	AuthorizeOutput func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.ResearchNode) error
	BeforeContinue  func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.Result, agentresult.Context, int64) error
	Specs           []graphexec.ActionSpec
	RegisterEffects func(*graphexec.Registry, *ActivityRegistry, *graphexec.Profile, func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), func() (string, string, error)) error
	BeforePublish   func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.Result, agentresult.Context) error
	Failure         func(Request, error) agentresult.Result
	Mode            agent.Mode
	Control         graphexec.ControlPolicy
	Decode          func([]byte, agentresult.Context) (agentresult.ResearchNode, agentresult.Diagnostics)
	Artifacts       func(string, string, agentresult.Result) error
	Guidance        string
	Contract        func(store.GraphNodeAttempt, string, agentresult.Result, agentresult.Context) error
	Render          func(agentresult.Result, string, naming.Ref, string) string
	NextStep        func(*graphexec.Profile, string, string) string
}

// ResearchWorkflowPolicy supplies Research contracts and scoped effects to shared mechanics.
func ResearchWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{AuthorizeOutput: authorizeResearchOutput, BeforeContinue: requireResearchBriefContinuation, Specs: []graphexec.ActionSpec{ResearchBriefSpec()}, RegisterEffects: registerResearchBrief, BeforePublish: requireResearchBrief, Failure: researchFailureCandidate, Mode: agent.ModeResearch, Control: graphexec.ResearchControlPolicy(), Decode: agentresult.DecodeResearchNode, Guidance: researchNodeGuidance(), Contract: validateResearchComment, Render: func(r agentresult.Result, next string, ref naming.Ref, branch string) string {
		return renderResearchComment(r, next, researchArtifactLinks(ref, branch, r.Artifacts))
	}, NextStep: researchNextStep, Artifacts: func(root, session string, r agentresult.Result) error {
		return ValidateResearchArtifactDeclarations(root, session, r.Artifacts)
	}}
}

func (p WorkflowPolicy) load(ctx context.Context, db *store.DB, id int64, scope agentresult.Context) (agentresult.ResearchNode, error) {
	node, err := loadNodeOutput(ctx, db, id, scope, p.Decode)
	if err != nil {
		return node, err
	}
	if p.AuthorizeOutput != nil {
		attempt, err := db.GetGraphNodeAttempt(ctx, id)
		if err != nil {
			return node, err
		}
		if err := p.AuthorizeOutput(ctx, db, attempt, node); err != nil {
			return agentresult.ResearchNode{}, err
		}
	}
	return node, nil
}

// WorkflowHandlers separates a mode's domain activities and invocation preparation
// from the shared graph lifecycle, native invocation, metering and saving
// machinery. Preparation must bind the shared result service and final route.
type WorkflowHandlers struct {
	Prepare    func(context.Context, *researchWorkflow, ActivityRequest) (ResearchInvocation, error)
	Control    ActivityHandler
	Comment    ActivityHandler
	BeforeSave func(context.Context, *researchWorkflow) error
	AfterSave  func(context.Context, *researchWorkflow, *Result) error
}

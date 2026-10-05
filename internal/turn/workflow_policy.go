package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
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

	// Respond maps a node's retained accepted result to the policy's routing
	// response. Nil uses the shared mapping: an incomplete result with questions
	// is ask, a completed result without questions is done, and any other shape
	// is refused. It reads only the retained result, never what the agent printed.
	// Bug maps its validated payload.bug.response through its Respond hook.
	Respond func(agentresult.Result) (string, error)

	AuthorizeOutput func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.ResearchNode) error
	BeforeContinue  func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.Result, agentresult.Context, int64) error
	Specs           []graphexec.ActionSpec
	RegisterEffects func(*graphexec.Registry, *ActivityRegistry, *graphexec.Profile, func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), func() (string, string, error), WorkflowEnv) error
	BeforePublish   func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.Result, agentresult.Context) error
	Failure         func(Request, error) agentresult.Result
	Mode            agent.Mode
	Workflow        agent.Workflow
	Control         graphexec.ControlPolicy
	Decode          func([]byte, agentresult.Context) (agentresult.ResearchNode, agentresult.Diagnostics)
	Artifacts       func(string, string, agentresult.Result) error
	Guidance        string
	Contract        func(store.GraphNodeAttempt, string, agentresult.Result, agentresult.Context) error
	Render          func(agentresult.Result, string, naming.Ref, string) string
	NextStep        func(*graphexec.Profile, string, string) string

	// AcceptAfterProcessFailure lets an agent step use a complete accepted result
	// of its own attempt when the process failed afterwards (non-zero exit, timeout
	// after submitting, transport close). A factory shutdown still interrupts it.
	AcceptAfterProcessFailure bool

	// ValidateCompletion is a mode's own extra check of a candidate result, run by
	// the shared service at preflight and submit. Nil adds nothing.
	ValidateCompletion func(context.Context, store.ResultBinding, agentresult.Result, agentresult.Context) agentresult.Diagnostics

	// Questions marks a mode whose graph runtime carries owner questions. Only
	// this factory-owned policy can grant it; it is still subject to an owner's
	// direct instruction not to ask.
	Questions bool

	// DefaultAccess is the tool access an agent node receives when its DOT
	// declares none: "read" narrows to read-only tools, empty leaves the run's
	// permission profile exactly as the daemon resolved it.
	DefaultAccess string

	// ResultPerNode gives every agent node of a graph instance an application
	// result of its own. Without it a graph instance authors one result that each
	// agent stage revises, which is what a single-role mode wants.
	ResultPerNode bool

	// Handoff supplies the saved context a later agent stage of the same graph
	// instance receives from the earlier stages' retained results. It reads
	// durable records only and returns untrusted data, never factory authority.
	// Nil supplies nothing.
	Handoff func(ctx context.Context, db *store.DB, instanceID, beforeAttempt int64, profile *graphexec.Profile, policy WorkflowPolicy, scope agentresult.Context) (string, error)

	// Annotate lets a mode add factory-recorded facts to the retained candidate a
	// comment is about to report, from durable records only. It runs for every
	// kind except failure; an error refuses the publication.
	Annotate func(ctx context.Context, db *store.DB, source store.GraphNodeAttempt, kind string, candidate agentresult.Result) (agentresult.Result, error)
}

// WorkflowEnv is the trusted run context a mode's scoped activities are built
// with: the task they belong to and the worktree the run holds.
type WorkflowEnv struct {
	Ref      naming.Ref
	Worktree func() (*git.Worktree, error)
}

// baseContext is the trusted result scope every graph node of this policy shares.
func (p WorkflowPolicy) baseContext(questions bool) agentresult.Context {
	return agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: string(p.Mode), WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: questions || p.Questions}
}

// ResearchWorkflowPolicy supplies Research contracts and scoped effects to shared mechanics.
func ResearchWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{Respond: researchRetainedRoute, AcceptAfterProcessFailure: true, ValidateCompletion: researchNodeSize, AuthorizeOutput: authorizeResearchOutput, BeforeContinue: requireResearchBriefContinuation, Specs: []graphexec.ActionSpec{ResearchBriefSpec()}, RegisterEffects: registerResearchBrief, BeforePublish: requireResearchBrief, Failure: researchFailureCandidate, Mode: agent.ModeResearch, Control: graphexec.ResearchControlPolicy(), Decode: agentresult.DecodeResearchNode, Guidance: researchNodeGuidance(), Contract: validateResearchComment, Render: func(r agentresult.Result, next string, ref naming.Ref, branch string) string {
		return renderResearchComment(r, next, researchArtifactLinks(ref, branch, r.Artifacts))
	}, NextStep: researchNextStep, Artifacts: func(root, session string, r agentresult.Result) error {
		return ValidateResearchArtifactDeclarations(root, session, r.Artifacts)
	}}
}

// researchNodeSize refuses a result whose kept encoding exceeds the 1 MiB node
// limit, so the agent learns it at preflight and submit rather than after the
// process ends. A done result is measured as approved, the longer kept response.
func researchNodeSize(_ context.Context, _ store.ResultBinding, r agentresult.Result, _ agentresult.Context) agentresult.Diagnostics {
	response, err := researchRetainedRoute(r)
	if err != nil {
		return agentresult.Diagnostics{Valid: true, Complete: true}
	}
	if response == "done" {
		response = "approved"
	}
	encoded, err := json.Marshal(agentresult.ResearchNode{Version: 1, Response: response, Result: r})
	if err != nil || len(encoded) <= agentresult.MaxResearchNodeBytes {
		return agentresult.Diagnostics{Valid: true, Complete: true}
	}
	return agentresult.Diagnostics{Complete: true, Diagnostics: []agentresult.Diagnostic{{Path: "$.result", Code: "limit", Expected: "kept result at most 1 MiB", Actual: fmt.Sprintf("%d bytes", len(encoded)), Message: "The result is larger than the 1 MiB Research keeps. Shorten it and refer to saved files for detail. Nothing was accepted."}}}
}

// routesRetained reports that all admitted graph policies route accepted data.
func (p WorkflowPolicy) routesRetained() bool { return true }

// respond derives the routing response from the retained accepted result.
func (p WorkflowPolicy) respond(r agentresult.Result) (string, error) {
	if p.Respond != nil {
		return p.Respond(r)
	}
	return retainedRoute(r)
}

// retainedRoute is the shared Research and Plan mapping from a retained result
// to its response. Approval is never derived here: only an authorized owner
// reply settles the displayed Research review at the factory wait boundary.
func retainedRoute(r agentresult.Result) (string, error) {
	switch {
	case r.Outcome == "incomplete" && len(r.Questions) > 0:
		return "ask", nil
	case r.Outcome == "completed" && len(r.Questions) == 0:
		return "done", nil
	}
	return "", fmt.Errorf("outcome %q with %d question(s) is neither ask (incomplete with owner questions) nor done (completed without questions)", r.Outcome, len(r.Questions))
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

func researchRetainedRoute(r agentresult.Result) (string, error) {
	if p := r.Payload.Research; p != nil && p.Continue {
		if r.Outcome == "incomplete" && strings.TrimSpace(r.Reason) != "" && len(p.Findings) > 0 && len(r.Questions) == 0 {
			return "continue", nil
		}
		return "", errors.New("invalid Research continuation")
	}
	return retainedRoute(r)
}

package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// BugWorkflowPolicy is Spike's policy in Bug mode. Bug is one agent: the
// factory publishes what it submitted and, when it asked, waits for the owner
// and resumes the same agent. Reproduction, diagnosis and the fix live in the
// role's Markdown; the flow lives in the graph file.
func BugWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Mode:                      agent.ModeBug,
		Control:                   graphexec.ControlPolicy{Mode: agent.ModeBug, Responses: []string{"ask", "done"}, Defaults: []string{"ask", "done"}, Ask: "ask"},
		Decode:                    agentresult.DecodeBugNode,
		Contract:                  validateResearchComment,
		Render:                    renderSpikeComment,
		NextStep:                  bugNextStep,
		Failure:                   bugFailureCandidate,
		Questions:                 true,
		AcceptAfterProcessFailure: true,
	}
}

// BugSpecs is the closed registry for prelaunch built-in asset validation.
func BugSpecs() (graphexec.Registry, error) {
	return WorkflowSpecs(BugWorkflowPolicy())
}

// bugFailureCandidate authors the owner-facing account of a failed Bug step.
func bugFailureCandidate(req Request, cause error) agentresult.Result {
	reason := "Bug failed before it produced a reportable result."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = boundedFailureReason(strings.TrimSpace(cause.Error()))
	}
	request := strings.TrimSpace(req.Trigger.Body)
	if request == "" || store.IsFactoryTrigger(req.Trigger.CommentID) {
		request = "The reported bug."
	}
	return agentresult.Result{
		Outcome: "blocked",
		Comment: "Bug did not finish. No fix is claimed. The reason is recorded below; comment again to start a fresh attempt.",
		Reason:  reason,
		Payload: agentresult.Payload{Bug: &agentresult.Spike{Request: request}},
	}
}

func bugNextStep(profile *graphexec.Profile, node, kind string) string {
	switch kind {
	case "question":
		return "reply on this issue to continue; the same Bug picks up where it left off. Nothing runs until you reply."
	case "final":
		return "this Bug is finished. Reply on this issue to ask more or to have something changed."
	}
	return researchNextStep(profile, node, kind)
}

// admitBugExecution admits Bug work on the shared workflow driver, as Spike's
// admission does. A bug-labelled issue's first instruction starts it, and an
// owner reply continues the instance that asked.
func admitBugExecution(ctx context.Context, opts Options, req Request, result Result) (Result, bool, error) {
	if req.mode() != string(agent.ModeBug) {
		return result, false, nil
	}
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		out, err := resumeBugDispatch(ctx, opts, req, result)
		return out, true, err
	}
	if req.Continuation != nil {
		return result, true, errors.New("bug graphs require explicit owner continuation")
	}
	if _, err := opts.DB.AdmitWorkflowPath(ctx, req.SessionID, "graph"); err != nil {
		return result, true, err
	}
	specs, err := BugSpecs()
	if err != nil {
		return result, true, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowBug, specs)
	if err != nil {
		return result, true, err
	}
	out, err := launchBugGraph(ctx, opts, req, result, definition)
	return out, true, err
}

// ExecuteBugWorkflow advances the captured Bug graph under Execute's task lock
// through the shared driver, with Bug's own factory policy.
func ExecuteBugWorkflow(ctx context.Context, opts Options, req Request, result Result, definition *teamsource.WorkflowDefinition, admission store.WorkflowAdmission) (Result, error) {
	return ExecuteWorkflow(ctx, opts, req, result, definition, admission, BugWorkflowPolicy())
}

// launchBugGraph pins a captured definition, admits the launch under the
// owner's authority and drives the shared workflow.
func launchBugGraph(ctx context.Context, opts Options, req Request, result Result, definition *teamsource.WorkflowDefinition) (Result, error) {
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return result, err
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		return result, err
	}
	pin, err := opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: definition.Profile.Team, Mode: string(agent.ModeBug), ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		return result, err
	}
	admitted, err := opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: string(agent.ModeBug), SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: req.Trigger.Body})
	if err != nil {
		return result, err
	}
	actualPin, err := opts.DB.GetGraphDefinition(ctx, admitted.Instance.DefinitionID)
	if err != nil {
		return result, err
	}
	definition.Profile.Digest = actualPin.Digest
	return ExecuteBugWorkflow(ctx, opts, req, result, definition, admitted)
}

// resumeBugDispatch enters the workflow at an instance's current node with the
// definition it was admitted under.
func resumeBugDispatch(ctx context.Context, opts Options, req Request, result Result) (Result, error) {
	instance, err := opts.DB.LatestWorkflowInstance(ctx, req.SessionID, string(agent.ModeBug))
	if err != nil {
		return result, fmt.Errorf("resume bug dispatch: %w", err)
	}
	if instance.Status != store.GraphRunning {
		return result, errors.New("bug graph dispatch targets an instance that is no longer running")
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return result, err
	}
	specs, err := BugSpecs()
	if err != nil {
		return result, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowBug, specs)
	if err != nil {
		return result, err
	}
	if definition.Profile.Digest != pin.Digest {
		return result, errors.New("retained bug graph does not match the running instance; the parked stage cannot resume")
	}
	return ExecuteBugWorkflow(ctx, opts, req, result, definition, retainedAdmission(ctx, opts.DB, req, instance, "bug"))
}

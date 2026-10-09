package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// spikeRetiredReason is the explicit refusal an owner reads when a task's Spike
// history belongs to the retired single-agent route. No agent has launched.
const spikeRetiredReason = "Spike refused: this task's earlier Spike work ran on the retired single-agent route, or its provenance is ambiguous, so it cannot continue. No agent launched and nothing was changed or migrated. Start a fresh graph Spike task with a new /spike request."

// admitSpikeExecution mirrors Research admission. It runs only for an already
// authorized manual launch, records the graph path once per task and hands the
// task to the shared workflow driver. Spike has no other route: a session whose
// history is retired legacy work, or cannot be told apart from it, is refused
// here, before any agent is dispatched, and is never rewritten onto the graph.
func admitSpikeExecution(ctx context.Context, opts Options, req Request, result Result) (Result, bool, error) {
	if req.mode() != string(agent.ModeSpike) {
		return result, false, nil
	}
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		out, err := resumeSpikeDispatch(ctx, opts, req, result)
		return out, true, err
	}
	if req.Continuation != nil {
		graph, err := opts.DB.HasWorkflowGraphSession(ctx, req.SessionID, string(agent.ModeSpike))
		if err != nil {
			return result, true, err
		}
		if graph {
			return result, true, errors.New("spike graphs require explicit owner continuation")
		}
		out, err := prelaunchRefused(ctx, opts, result, spikeRetiredReason)
		return out, true, err
	}
	if err := opts.DB.AdmitSpikePath(ctx, req.SessionID); err != nil {
		if errors.Is(err, store.ErrSpikeLegacyRetired) {
			out, refuseErr := prelaunchRefused(ctx, opts, result, spikeRetiredReason)
			return out, true, refuseErr
		}
		return result, true, err
	}
	specs, err := SpikeSpecs()
	if err != nil {
		return result, true, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowSpike, specs)
	if err != nil {
		return result, true, err
	}
	out, err := launchSpikeGraph(ctx, opts, req, result, definition)
	return out, true, err
}

// ExecuteSpikeWorkflow advances the captured Spike graph under Execute's task
// lock through the shared driver, with Spike's own factory policy.
func ExecuteSpikeWorkflow(ctx context.Context, opts Options, req Request, result Result, definition *teamsource.WorkflowDefinition, admission store.WorkflowAdmission) (Result, error) {
	return ExecuteWorkflow(ctx, opts, req, result, definition, admission, SpikeWorkflowPolicy())
}

// launchSpikeGraph pins a captured definition, admits the launch under the
// owner's authority and drives the shared workflow. The definition is always the
// one the caller captured; nothing here reads the flow.
func launchSpikeGraph(ctx context.Context, opts Options, req Request, result Result, definition *teamsource.WorkflowDefinition) (Result, error) {
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return result, err
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		return result, err
	}
	pin, err := opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: definition.Profile.Team, Mode: string(agent.ModeSpike), ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		return result, err
	}
	admitted, err := opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: string(agent.ModeSpike), SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: req.Trigger.Body})
	if err != nil {
		return result, err
	}
	// Equivalent topology retains the earlier pin; current instruction bytes
	// remain separately captured and drive effective delivery identity.
	actualPin, err := opts.DB.GetGraphDefinition(ctx, admitted.Instance.DefinitionID)
	if err != nil {
		return result, err
	}
	definition.Profile.Digest = actualPin.Digest
	return ExecuteSpikeWorkflow(ctx, opts, req, result, definition, admitted)
}

// resumeSpikeDispatch enters the workflow at an instance's current node with the
// definition it was admitted under. Nothing here is a fresh admission.
func resumeSpikeDispatch(ctx context.Context, opts Options, req Request, result Result) (Result, error) {
	instance, err := opts.DB.LatestWorkflowInstance(ctx, req.SessionID, string(agent.ModeSpike))
	if err != nil {
		return result, fmt.Errorf("resume spike dispatch: %w", err)
	}
	if instance.Status != store.GraphRunning {
		return result, errors.New("spike graph dispatch targets an instance that is no longer running")
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return result, err
	}
	specs, err := SpikeSpecs()
	if err != nil {
		return result, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowSpike, specs)
	if err != nil {
		return result, err
	}
	if definition.Profile.Digest != pin.Digest {
		return result, errors.New("retained spike graph does not match the running instance; the parked stage cannot resume")
	}
	return ExecuteSpikeWorkflow(ctx, opts, req, result, definition, retainedAdmission(ctx, opts.DB, req, instance, "spike"))
}

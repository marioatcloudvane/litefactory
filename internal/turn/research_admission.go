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

// ResearchLaunchDriver executes the admitted DOT under Execute's task lock.
// It owns graph preparation and explicit publication, bypassing legacy finalization.
type ResearchLaunchDriver func(context.Context, Options, Request, Result, *teamsource.ResearchDefinition, store.ResearchAdmission) (Result, error)

// ResearchSpecs is the closed factory registry for prelaunch workflow validation.
func ResearchSpecs() (graphexec.Registry, error) {
	return WorkflowSpecs(ResearchWorkflowPolicy())
}

// WorkflowSpecs registers generic mechanisms only for the explicitly admitted policy.
func WorkflowSpecs(policy WorkflowPolicy) (graphexec.Registry, error) {
	specs := graphexec.Registry{Policy: func(p *graphexec.Profile) error { return graphexec.ValidateControlGraph(p, policy.Control) }}
	if err := policy.Admit(); err != nil {
		return specs, fmt.Errorf("workflow registry requires consistent admitted mode and result policy: %w", err)
	}
	comments, asks := ResearchCommentSpec(), ResearchAskSpec()
	comments.Mode, asks.Mode = policy.Mode, policy.Mode
	comments.Workflow, asks.Workflow = policy.Workflow, policy.Workflow
	for _, spec := range []graphexec.ActionSpec{
		{Name: ResearchAgentAction, Mode: policy.Mode, Workflow: policy.Workflow, Outcomes: []string{"success", "failed"}, Attributes: graphexec.AgentAttributes()},
		graphexec.ControlSpec(graphexec.ResearchLoopAction, policy.Control),
		graphexec.ControlSpec(graphexec.ResearchIfAction, policy.Control), comments, asks,
	} {
		if err := specs.Register(spec); err != nil {
			return specs, err
		}
	}
	for _, spec := range policy.Specs {
		if err := specs.Register(spec); err != nil {
			return specs, err
		}
	}
	return specs, nil
}

// admitResearchExecution runs only for an already authorized manual launch.
// Reconciliation and quota-recovery paths cannot supply invocation authority.
func admitResearchExecution(ctx context.Context, opts Options, req Request, result Result) (Result, bool, error) {
	if req.mode() != "research" {
		return result, false, nil
	}
	// A graph dispatch is the factory continuing work it already admitted. It
	// must not manufacture owner authority. The retained graph supplies its
	// edges while invocation and effect identities stay historical.
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		out, err := resumeResearchDispatch(ctx, opts, req, result)
		return out, true, err
	}
	if req.Continuation != nil {
		return result, true, errors.New("research requires graph continuation")
	}
	selected, err := opts.DB.AdmitResearchPath(ctx, req.SessionID, "graph")
	if err != nil {
		return result, true, err
	}
	if selected != "graph" {
		return result, true, errors.New("legacy Research task requires manual operator migration")
	}
	if opts.ResearchDriver == nil {
		return result, true, errors.New("research graph execution is selected but the application graph driver is unavailable")
	}
	specs, err := ResearchSpecs()
	if err != nil {
		return result, true, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowResearch, specs)
	if err != nil {
		return result, true, fmt.Errorf("capture research workflow: %w", err)
	}
	if err = graphexec.ValidateResearchControlGraph(definition.Profile); err != nil {
		return result, true, err
	}
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return result, true, err
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		return result, true, err
	}
	pin, err := opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: definition.Profile.Team, Mode: "research", ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		return result, true, err
	}
	compatible := false
	prior, priorErr := opts.DB.LatestResearchInstance(ctx, req.SessionID)
	if priorErr != nil && !errors.Is(priorErr, store.ErrNotFound) {
		return result, true, priorErr
	}
	if priorErr == nil && prior.Status == store.GraphSucceeded {
		wait, waitErr := opts.DB.GetGraphOwnerWait(ctx, prior.ID)
		if waitErr != nil && !errors.Is(waitErr, store.ErrNotFound) {
			return result, true, waitErr
		}
		if waitErr == nil && wait.State == "allocated" && wait.ContinuationAttemptID == 0 && wait.ReplyID == req.Trigger.CommentID {
			result.ResearchGraph = true
			result.Outcome, result.Disposition = agent.OutcomeSucceeded, DispositionRan
			return result, true, nil
		}
	}
	if priorErr == nil && prior.Status == store.GraphRunning {
		compatible, err = researchNodeCompatible(ctx, opts.DB, prior, definition.Profile)
		if err != nil {
			return result, true, err
		}
	}
	admitted, err := opts.DB.AdmitResearchLaunch(ctx, store.ResearchLaunch{SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: req.Trigger.Body, ResearchCompatible: compatible})
	if err != nil {
		return result, true, err
	}
	out, err := opts.ResearchDriver(ctx, opts, req, result, definition, admitted)
	return out, true, err
}

// resumeResearchDispatch enters the workflow at an instance's current node with
// its verified retained definition. Nothing here is a fresh owner admission: the
// dispatch that allocated this run is the authority, and it exists only because
// a previous run of this instance published and had no run left to continue in.
func resumeResearchDispatch(ctx context.Context, opts Options, req Request, result Result) (Result, error) {
	if opts.ResearchDriver == nil {
		return result, errors.New("research graph dispatch requires the application graph driver")
	}
	instance, err := opts.DB.LatestResearchInstance(ctx, req.SessionID)
	if err != nil {
		return result, fmt.Errorf("resume research dispatch: %w", err)
	}
	if instance.Status != store.GraphRunning {
		return result, errors.New("research graph dispatch targets an instance that is no longer running")
	}
	specs, err := ResearchSpecs()
	if err != nil {
		return result, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowResearch, specs)
	if err != nil {
		return result, fmt.Errorf("capture research workflow: %w", err)
	}
	saved, err := opts.DB.WorkflowCaptureForAdmission(ctx, req.SessionID, req.Trigger.CommentID, "research")
	if err != nil {
		return result, err
	}
	admission := store.ResearchAdmission{Instance: instance, LaunchID: saved.ID}
	return opts.ResearchDriver(ctx, opts, req, result, definition, admission)
}

// researchNodeCompatible compares contracts, not digests or graph edges.
func researchNodeCompatible(ctx context.Context, db *store.DB, instance store.GraphInstance, current *graphexec.Profile) (bool, error) {
	if current.Mode != agent.ModeResearch {
		return false, errors.New("research recovery requires Research profile")
	}
	files, err := db.ResearchLaunchFiles(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		_, found := current.Node(instance.CurrentNode)
		return found, nil
	}
	if err != nil {
		return false, err
	}
	var captured map[string][]byte
	if err := json.Unmarshal(files, &captured); err != nil {
		return false, err
	}
	graphFile, err := retainedGraphFile(ctx, db, instance.ID)
	if err != nil {
		return false, err
	}
	raw, present := captured[graphFile]
	if !present {
		_, found := current.Node(instance.CurrentNode)
		return found, nil
	}
	old, err := graphexec.ParseProfile(raw)
	if err != nil {
		return false, fmt.Errorf("read historical Research graph: %w", err)
	}
	previous, existed := old.Node(instance.CurrentNode)
	next, found := current.Node(instance.CurrentNode)
	return existed && found && previous.Action == next.Action && previous.Terminal == next.Terminal && previous.Attrs["lf_source"] == next.Attrs["lf_source"] && previous.Attrs["lf_kind"] == next.Attrs["lf_kind"], nil
}

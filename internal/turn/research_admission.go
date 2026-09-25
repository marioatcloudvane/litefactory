package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// ResearchLaunchDriver executes the admitted DOT under Execute's task lock.
// It owns graph preparation and explicit publication, bypassing legacy finalization.
type ResearchLaunchDriver func(context.Context, Options, Request, Result, *teamsource.ResearchDefinition, store.ResearchAdmission) (Result, error)

// researchDefaultActive is deliberately false in this delivered application.
// Authentic assembled host evidence must accompany an explicit activation
// change. No owner text, project guidance or repository option can set this gate.
const researchDefaultActive = false

// ResearchSpecs is the closed registry for prelaunch built-in asset validation.
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
	for _, spec := range []graphexec.ActionSpec{
		{Name: ResearchAgentAction, Mode: policy.Mode, Outcomes: []string{"success", "failed"}},
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
	// must not re-admit: admission is owner authority, and re-capturing here
	// would rotate the definition under a running instance. Resume the pinned
	// one at the node it was parked on.
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		out, err := resumeResearchDispatch(ctx, opts, req, result)
		return out, true, err
	}
	if req.Continuation != nil {
		graph, err := opts.DB.HasResearchGraphSession(ctx, req.SessionID)
		if err != nil {
			return result, true, err
		}
		if graph {
			return result, true, errors.New("research graphs require explicit owner continuation")
		}
		return result, false, nil
	}
	repo, err := opts.DB.GetRepo(ctx, result.Session.RepoID)
	if err != nil {
		return result, true, err
	}
	selected, err := config.ResearchExecution(repo.Path)
	if err != nil {
		return result, true, err
	}
	if selected == "" {
		selected = "legacy"
		if researchDefaultActive {
			selected = "graph"
		}
	}
	selected, err = opts.DB.AdmitResearchPath(ctx, req.SessionID, selected)
	if err != nil {
		return result, true, err
	}
	if selected == "legacy" {
		return result, false, nil
	}
	if opts.ResearchDriver == nil {
		return result, true, errors.New("research graph execution is selected but the application graph driver is unavailable")
	}
	specs, err := ResearchSpecs()
	if err != nil {
		return result, true, err
	}
	definition, err := teamsource.CaptureResearch(ctx, specs)
	if err != nil {
		return result, true, fmt.Errorf("capture current shipped Research: %w", err)
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
	pin, err := opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: definition.Profile.ProfileVersion, Path: "research/research.dot", Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		return result, true, err
	}
	admitted, err := opts.DB.AdmitResearchLaunch(ctx, store.ResearchLaunch{SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Reply: req.Trigger.Body})
	if err != nil {
		return result, true, err
	}
	// Equivalent topology retains the foundation pin. Current instruction bytes
	// remain separately captured above and drive effective delivery identity.
	actualPin, err := opts.DB.GetGraphDefinition(ctx, admitted.Instance.DefinitionID)
	if err != nil {
		return result, true, err
	}
	definition.Profile.Digest = actualPin.Digest
	out, err := opts.ResearchDriver(ctx, opts, req, result, definition, admitted)
	return out, true, err
}

// resumeResearchDispatch enters the workflow at an instance's current node with
// the definition it was admitted under. Nothing here is a fresh admission: the
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
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return result, err
	}
	specs, err := ResearchSpecs()
	if err != nil {
		return result, err
	}
	definition, err := teamsource.CaptureResearch(ctx, specs)
	if err != nil {
		return result, fmt.Errorf("capture current shipped Research: %w", err)
	}
	// The running instance keeps the definition it was pinned to. Shipped bytes
	// may have changed since; that rotates a later launch, never this one.
	if definition.Profile.Digest != pin.Digest {
		return result, errors.New("shipped Research graph changed under a running instance; the parked stage cannot resume")
	}
	definition.Profile.Digest = pin.Digest
	return opts.ResearchDriver(ctx, opts, req, result, definition, store.ResearchAdmission{Instance: instance})
}

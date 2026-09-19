package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
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
	var specs graphexec.Registry
	for _, spec := range []graphexec.ActionSpec{
		{Name: ResearchAgentAction, Mode: agent.ModeResearch, Outcomes: []string{"success"}},
		graphexec.ResearchControlSpec(graphexec.ResearchLoopAction),
		graphexec.ResearchControlSpec(graphexec.ResearchIfAction),
		ResearchCommentSpec(),
	} {
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

package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResearchControlActivity owns the immutable profile and factory context used
// to revalidate retained application results. Neither is supplied by node JSON.
type ResearchControlActivity struct {
	Profile *graphexec.Profile
	Context func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
}

// RegisterResearchControl registers both closed control actions. Durable limits
// are enforced by ExecuteNodeAction's existing atomic attempt reservation.
func RegisterResearchControl(specs *graphexec.Registry, activities *ActivityRegistry, handler ResearchControlActivity) error {
	if specs == nil || activities == nil || handler.Profile == nil || handler.Context == nil {
		return errors.New("research control requires registries, pinned profile and result context")
	}
	if err := graphexec.ValidateResearchControlGraph(handler.Profile); err != nil {
		return err
	}
	for _, name := range []string{graphexec.ResearchLoopAction, graphexec.ResearchIfAction} {
		if _, exists := specs.Resolve(name); exists {
			return fmt.Errorf("research control %q already registered", name)
		}
		if _, exists := activities.Resolve(name); exists {
			return fmt.Errorf("research control handler %q already registered", name)
		}
	}
	for _, name := range []string{graphexec.ResearchLoopAction, graphexec.ResearchIfAction} {
		if err := specs.Register(graphexec.ResearchControlSpec(name)); err != nil {
			return err
		}
		if err := activities.Register(name, handler); err != nil {
			return err
		}
	}
	return nil
}

// Execute evaluates only validated retained output under the pinned control definition.
func (h ResearchControlActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Context == nil || req.Mode != agent.ModeResearch {
		return ActivityResult{}, errors.New("research control requires pinned Research context")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	if definition.Digest != h.Profile.Digest || definition.Mode != "research" {
		return ActivityResult{}, errors.New("research control profile differs from pinned definition")
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok {
		return ActivityResult{}, errors.New("research control node is absent")
	}
	condition, err := graphexec.DecodeResearchCondition(node.Action, node.Attrs)
	if err != nil {
		return ActivityResult{}, err
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if attempt.InstanceID != req.InstanceID || attempt.Node != req.Node || attempt.Status != store.GraphRunning || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("research control attempt binding mismatch")
	}
	var latest store.GraphNodeAttempt
	for _, source := range h.Profile.Nodes {
		if source.Action != ResearchAgentAction {
			continue
		}
		candidate, err := opts.DB.LatestGraphNodeAttempt(ctx, req.InstanceID, source.Name)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return ActivityResult{}, err
		}
		if candidate.ID > latest.ID {
			latest = candidate
		}
	}
	var response string
	if latest.ID != 0 {
		scope, err := h.Context(ctx, latest)
		if err != nil {
			return ActivityResult{}, err
		}
		result, err := LoadResearchNodeOutput(ctx, opts.DB, latest.ID, scope)
		if err != nil {
			return ActivityResult{}, fmt.Errorf("research control retained output: %w", err)
		}
		response = result.Response
	}
	// A repeated control pass with no agent result must never reuse the default.
	attempted := latest.ID != 0 || attempt.Attempt > 1
	outcome, err := condition.Evaluate(response, attempted)
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: outcome}, nil
}

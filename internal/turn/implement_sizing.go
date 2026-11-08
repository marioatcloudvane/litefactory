package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// sizingFact reads the approved generation, never the current parent Plan. Each
// condition retains both its evaluated inputs and their immutable source.
func (rt *implementRuntime) sizingFact(ctx context.Context, opts Options, a ActivityRequest) (ActivityResult, error) {
	if rt.worker == nil || rt.definition == nil || opts.DB == nil || a.Mode != agent.ModeImplement {
		return ActivityResult{}, errors.New("implement sizing requires a pinned story worker")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, a.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	node, exists := rt.definition.Profile.Node(a.Node)
	if !exists || node.Action != graphexec.ResearchIfAction || instance.SessionID != rt.worker.WorkerSession || pin.Digest != rt.definition.Profile.Digest || pin.Mode != string(agent.ModeImplement) {
		return ActivityResult{}, errors.New("implement sizing differs from pinned story graph")
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, a.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if attempt.InstanceID != instance.ID || attempt.Node != node.Name || attempt.Status != store.GraphRunning || instance.CurrentNode != node.Name {
		return ActivityResult{}, errors.New("implement sizing attempt binding mismatch")
	}
	condition, err := graphexec.DecodeCondition(node.Action, node.Attrs, graphexec.ImplementControlPolicy())
	if err != nil {
		return ActivityResult{}, err
	}
	revision, err := implementStoryRevision(ctx, opts.DB, *rt.worker)
	if err != nil {
		return ActivityResult{}, err
	}
	sizing, err := opts.DB.GetApprovedStorySizing(ctx, rt.worker.Parent, int(rt.worker.Generation), rt.worker.Key, revision.Hash)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("read approved story sizing: %w", err)
	}
	facts, explanation := implementSizingFacts(sizing)
	outcome, err := condition.EvaluateImplementFacts(facts)
	if err != nil {
		return ActivityResult{}, err
	}
	return implementResult(outcome, map[string]any{
		"facts": facts, "explanation": explanation, "source": sizing,
		"parent": rt.worker.Parent, "generation": rt.worker.Generation,
		"story": rt.worker.Key, "approved_digest": revision.Hash,
		"field": condition.Field, "condition": condition.Condition, "outcome": outcome,
	})
}

func implementSizingFacts(sizing *store.ApprovedStorySizing) (map[string]string, string) {
	facts := map[string]string{"story.sizing_valid": "false", "story.size": ""}
	if sizing == nil {
		return facts, "approved sizing unavailable"
	}
	facts["story.size"] = sizing.Size
	if !sizing.Available {
		return facts, sizing.Explanation
	}
	valid, explanation := planbrief.ValidateStorySizing(sizing.Size, sizing.SizeReason)
	facts["story.sizing_valid"] = fmt.Sprint(valid)
	return facts, explanation
}

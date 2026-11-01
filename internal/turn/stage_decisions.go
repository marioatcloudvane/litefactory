package turn

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// StageDecisionBoundary composes retained workflow scope with atomic store admission.
type StageDecisionBoundary struct{ DB *store.DB }

// ResolveStageDecisionTarget restores the retained scope for assembled Plan review.
func (b StageDecisionBoundary) ResolveStageDecisionTarget(ctx context.Context, task, stage, operation string) (store.StageDecisionTarget, error) {
	target, err := b.DB.ResolveStageDecisionTarget(ctx, task, stage, operation)
	if err != nil {
		return target, err
	}
	if target.Workflow == "plan" && target.Stage == "assembled" {
		scope, ok, e := RetainedPlanContentScope(ctx, b.DB, target.PlanParent)
		if e != nil {
			return target, e
		}
		if !ok {
			return target, store.ErrStageDecisionTarget
		}
		target.Scope = scope
	}
	return target, nil
}

// ApplyStageDecision delegates to the guarded atomic owner transition.
func (b StageDecisionBoundary) ApplyStageDecision(ctx context.Context, t store.StageDecisionTarget, change, key string, at time.Time) (store.StageDecisionReceipt, error) {
	return b.DB.ApplyStageDecision(ctx, t, change, key, at)
}

// RecoverStageDecision reads a receipt or fences a proven uncommitted execution.
func (b StageDecisionBoundary) RecoverStageDecision(ctx context.Context, key string, at time.Time) (store.StageDecisionReceipt, bool, error) {
	return b.DB.RecoverStageDecision(ctx, key, at)
}

// retainedStageRevision validates private dispatch provenance against the receipt.
// The trigger carries a reference; every field is read again from the database.
func retainedStageRevision(ctx context.Context, db *store.DB, trigger Trigger, task string) (store.StageDecisionContinuation, bool, error) {
	id, ok := store.GraphTriggerDispatch(trigger.CommentID)
	if !ok || trigger.StageDecisionRef == "" {
		return store.StageDecisionContinuation{}, false, nil
	}
	c, found, err := db.StageDecisionForDispatch(ctx, id)
	if err != nil {
		return c, false, err
	}
	if !found {
		return c, false, store.ErrStageDecisionTarget
	}
	if c.ExecutionID != trigger.StageDecisionRef || c.TaskRef != task || c.Operation != "revise_task" || c.OwnerID != trigger.AuthorID || c.Change != trigger.Body {
		return c, false, store.ErrStageDecisionTarget
	}
	return c, true, nil
}

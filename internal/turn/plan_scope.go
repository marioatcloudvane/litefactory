package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// RetainedPlanContentScope restores the exact last retained Plan attempt's scope
// for read-only scheduling, reconciliation and legacy contract maintenance.
// It never reads a current team catalogue or infers authority from story content.
func RetainedPlanContentScope(ctx context.Context, db *store.DB, parent string) (planbrief.ContentScope, bool, error) {
	session, err := db.GetSessionByTaskRef(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return planbrief.ContentScope{}, false, nil
	}
	if err != nil {
		return planbrief.ContentScope{}, false, err
	}
	instance, err := db.LatestWorkflowInstance(ctx, session.ID, "plan")
	if errors.Is(err, store.ErrNotFound) {
		return planbrief.ContentScope{}, false, nil
	}
	if err != nil {
		return planbrief.ContentScope{}, false, err
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return planbrief.ContentScope{}, false, err
	}
	if len(attempts) == 0 {
		return planbrief.ContentScope{}, false, nil
	}
	saved, err := db.WorkflowCaptureForAttempt(ctx, attempts[len(attempts)-1].ID)
	if err != nil {
		return planbrief.ContentScope{}, false, err
	}
	specs, err := PlanSpecs()
	if err != nil {
		return planbrief.ContentScope{}, false, err
	}
	definition, err := restoreWorkflow(ctx, db, saved, agent.WorkflowPlan, specs)
	if err != nil {
		return planbrief.ContentScope{}, false, err
	}
	scope, err := PlanContentScope(definition)
	return scope, err == nil, err
}

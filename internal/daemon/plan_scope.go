package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// planContentScope uses retained authority for maintenance of saved Plan state.
// Only a Plan with no saved capture resolves a fresh linked release.
func (d *Daemon) planContentScope(ctx context.Context, t *task) (planbrief.ContentScope, error) {
	scope, found, err := turn.RetainedPlanContentScope(ctx, d.db, t.ref.String())
	if err != nil || found {
		return scope, err
	}
	executor, ok := d.turn.(turnExecutor)
	if ok && executor.opts.WorkflowSource != nil {
		specs, err := turn.PlanSpecs()
		if err != nil {
			return planbrief.ContentScope{}, err
		}
		definition, err := executor.opts.WorkflowSource(ctx, t.repo, agent.WorkflowPlan, specs)
		if err != nil {
			return planbrief.ContentScope{}, err
		}
		return turn.PlanContentScope(definition)
	}
	// Pre-profile saved Plan maintenance has no source resolver or binding. This
	// explicit compatibility scope cannot authorize a newly linked team.
	if _, err := d.db.GetRepositoryTeamBinding(ctx, t.repo.ID); errors.Is(err, store.ErrNotFound) {
		return planbrief.TaskOwnerScope(), nil
	} else if err != nil {
		return planbrief.ContentScope{}, err
	}
	return planbrief.ContentScope{}, errors.New("plan scope: linked release resolver is required")
}

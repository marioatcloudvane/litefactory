package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// graphPlanParents admits retained drafts even before managed publication and
// without owner comments. The existing worker scheduler supplies capacity/backoff.
func (d *Daemon) graphPlanParents(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	sessions, err := d.db.ListGraphPlanSessions(ctx)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		q := byRepo[s.RepoID]
		if q == nil {
			continue
		}
		if prior := seen[s.ID]; prior != nil {
			prior.addSource("plan_recovery")
			continue
		}
		ref, err := naming.ParseRef(s.TaskRef)
		if err != nil {
			return err
		}
		c := q.candidate(s, ref)
		c.addSource("plan_recovery")
		q.candidates = append(q.candidates, c)
		seen[s.ID] = c
	}
	return nil
}

// recoverPlanStories runs on the parent's ordinary admitted worker, before child
// input routing. It never runs another agent or recursively takes a parent lock.
func (d *Daemon) recoverPlanStories(ctx context.Context, t *task) error {
	route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if route != "graph" {
		return nil
	}
	executor, ok := d.turn.(turnExecutor)
	if !ok {
		return nil
	}
	runtime := executor.opts.MCP
	if runtime == nil {
		return errors.New("plan recovery requires factory runtime")
	}
	forge, ok := d.forge.(turn.PlanStoryForge)
	if !ok {
		return errors.New("plan recovery requires story forge")
	}
	scope, err := d.planContentScope(ctx, t)
	if err != nil {
		return err
	}
	h := turn.PlanStoryActivity{DB: d.db, Forge: forge, Redactor: runtime.Redactor, LockDir: runtime.Paths.StoryProgressLocksDir()}
	return h.Reconcile(ctx, turn.PlanStoryAuthority{Parent: t.ref.String(), Repo: github.Repo{Owner: t.ref.Owner, Name: t.ref.Repo}, ParentNumber: t.issue.Number, ParentID: t.issue.ID, OwnerID: d.ticketIdentities(ctx).OwnerUserID, ContentScope: scope}, 8)
}

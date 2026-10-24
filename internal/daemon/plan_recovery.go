package daemon

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// graphPlanParents admits retained drafts even before managed publication and
// without owner comments. The existing worker scheduler supplies capacity/backoff.
// A Linear parent is re-added only on its repository's listing ticks (#920).
// On a repository with a healthy webhook a parent is re-added between listings
// only when it has due Plan work (#1138); its new input otherwise arrives as a
// webhook hint, ticket observation or backlog flag.
func (d *Daemon) graphPlanParents(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	sessions, err := d.db.ListGraphPlanSessions(ctx)
	if err != nil {
		return err
	}
	linearRepo, hookedRepo := map[int64]bool{}, map[int64]bool{}
	for _, s := range sessions {
		q := byRepo[s.RepoID]
		if q == nil {
			continue
		}
		if prior := seen[s.ID]; prior != nil {
			prior.addSource("plan_recovery")
			continue
		}
		if skip, err := d.linearOffListing(ctx, q, s.RepoID, linearRepo); err != nil {
			return err
		} else if skip {
			continue
		}
		ref, err := naming.ParseRef(s.TaskRef)
		if err != nil {
			return err
		}
		if skip, err := d.webhookOffListing(ctx, q, hookedRepo); err != nil {
			return err
		} else if skip {
			if due, err := d.planParentDue(ctx, q, s, ref); err != nil {
				return err
			} else if !due {
				continue
			}
		}
		c := q.candidate(s, ref)
		c.addSource("plan_recovery")
		q.candidates = append(q.candidates, c)
		seen[s.ID] = c
	}
	return nil
}

// planParentDue reports whether a graph Plan parent's worker has Plan work to
// do now: an assembled review that is due and not yet attempted, recovery debt,
// a promoted story still owed its issue shell, or a story that owes a comment
// sweep. A failure to read any of it counts as due, so the parent is offered as
// before rather than left waiting for a listing.
func (d *Daemon) planParentDue(ctx context.Context, q *repoQueue, s store.Session, ref naming.Ref) (bool, error) {
	due, err := d.planParentDueWork(ctx, q, s, ref)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		q.log.DebugContext(ctx, "could not tell whether a Plan parent has due work; offering it",
			slog.String("task", ref.String()), slog.Any("error", err))
		return true, nil
	}
	return due, nil
}

func (d *Daemon) planParentDueWork(ctx context.Context, q *repoQueue, s store.Session, ref naming.Ref) (bool, error) {
	parent := ref.String()
	t := &task{repo: q.repo, forgeRepo: q.forgeRepo, session: s, ref: ref, issue: github.Issue{Number: ref.Issue}, log: q.log}
	if id, ok, err := d.planWorkFor(ctx, t); err != nil {
		return false, err
	} else if ok {
		attempted, err := d.db.ScheduleAttempted(ctx, s.ID, id)
		if err != nil || !attempted {
			return err == nil, err
		}
	}
	if debt, err := d.db.PlanStoryRecoveryDebt(ctx, parent, 1); err != nil || len(debt) > 0 {
		return err == nil, err
	}
	if owed, err := d.planShellsOwed(ctx, parent); err != nil || owed {
		return err == nil, err
	}
	feature, err := d.db.GetManagedFeature(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	awaiting, err := d.db.ListStoriesAwaitingDiscovery(ctx, s.ID, parent, feature.Generation)
	return len(awaiting) > 0, err
}

// planShellsOwed is the check ReconcileShells makes before it creates story
// issues: the story set is promoted and an active, non-retired draft has no
// candidate yet.
func (d *Daemon) planShellsOwed(ctx context.Context, parent string) (bool, error) {
	state, _, err := d.db.GetPlanWorkflowState(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	set := state.StorySet
	if set == nil || !set.Promoted {
		return false, nil
	}
	active := map[string]bool{}
	for _, story := range state.Stories {
		active[story.Key] = story.Active()
	}
	for _, entry := range set.Entries {
		draft, err := d.db.GetPlanStoryDraft(ctx, parent, set.Keys[entry.ID])
		if err != nil {
			return false, err
		}
		if active[draft.Key] && !draft.Retired && draft.Candidate == "" {
			return true, nil
		}
	}
	return false, nil
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
	authority := turn.PlanStoryAuthority{Parent: t.ref.String(), Repo: github.Repo{Owner: t.ref.Owner, Name: t.ref.Repo}, ParentNumber: t.issue.Number, ParentID: t.issue.ID, OwnerID: d.ticketIdentities(ctx).OwnerUserID, ContentScope: scope}
	frozen, err := d.planFrozen(ctx, t.ref.String())
	if err != nil {
		return err
	}
	if !frozen {
		if err := h.ReconcileShells(ctx, authority); err != nil {
			return err
		}
	}
	return h.Reconcile(ctx, authority, 8)
}

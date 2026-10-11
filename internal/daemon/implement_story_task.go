package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// implementWorkPayload is the instruction of a run the factory starts by itself
// for an Implement story worker. The story, its branch and everything else the
// run needs come from the ledger, never from this text.
const implementWorkPayload = "Continue the Implement work of this issue."

// maxImplementInstanceFailures is how many worker runs of one story may end on an
// infrastructure fault before discovery stops starting another. A reply from the
// owner resumes the story and counts from the start again.
const maxImplementInstanceFailures = 3

// implementWorkID is the identity of one piece of factory-started story work. It
// is a pure function of the story, when it last changed state and the worker
// instance it follows, so the same work is the same schedule: a story that is
// still running is not started again, a failed worker run is retried once, and a
// resumed story is new work.
func implementWorkID(row store.ImplementStory, instanceID int64) int64 {
	return planWorkID("implement", row.Parent, strconv.FormatInt(row.Generation, 10), row.Key,
		strconv.FormatInt(row.StateAt.UnixNano(), 10), strconv.FormatInt(instanceID, 10))
}

// implementRetryWorkID is the identity of the retry-th relaunch of the same
// story work after earlier launches failed before any worker instance started.
// Retry 0 is implementWorkID itself, so existing schedules keep their identity.
func implementRetryWorkID(row store.ImplementStory, instanceID int64, retry int) int64 {
	if retry == 0 {
		return implementWorkID(row, instanceID)
	}
	return planWorkID("implement", row.Parent, strconv.FormatInt(row.Generation, 10), row.Key,
		strconv.FormatInt(row.StateAt.UnixNano(), 10), strconv.FormatInt(instanceID, 10),
		"retry", strconv.Itoa(retry))
}

// implementWork reports the identity of the worker run a running story is owed
// now. A launch whose schedule failed before any worker instance started leaves
// nothing behind that changes implementWorkID, so each such refused launch moves
// the work to the next retry identity. Refused launches count toward the same
// failure limit as failed instances, and a story that reaches it is parked. A
// schedule still pending, running or finished otherwise stops the walk, so work
// in flight is never started twice. A story parked here is parked at the
// instant the tick began.
func (d *Daemon) implementWork(ctx context.Context, row store.ImplementStory, at time.Time) (int64, bool, error) {
	instanceID, ok, err := d.implementWorkable(ctx, row)
	if err != nil || !ok {
		return 0, false, err
	}
	if row.WorkerSession == 0 {
		return implementWorkID(row, instanceID), true, nil
	}
	failed := 0
	if instanceID != 0 {
		if failed, err = d.db.CountFailedImplementInstances(ctx, row.WorkerSession, row.StateAt); err != nil {
			return 0, false, err
		}
	}
	for refused := 0; ; refused++ {
		if failed+refused >= maxImplementInstanceFailures {
			reason := row.LastFailure
			if reason == "" {
				reason = fmt.Sprintf("The worker launch was refused %d times before any worker instance started.", refused)
			}
			_, err := d.db.TransitionImplementStory(ctx, store.ImplementTransition{
				Parent: row.Parent, Generation: row.Generation, Key: row.Key,
				From: store.ImplementRunning, To: store.ImplementParked,
				ExpectedRevision: row.Revision, Reason: reason, At: at.UTC(),
			})
			if errors.Is(err, store.ErrImplementConflict) {
				err = nil // Another discovery or owner transition already moved it.
			}
			return 0, false, err
		}
		id := implementRetryWorkID(row, instanceID, refused)
		status, found, err := d.db.ScheduleStatusBySource(ctx, row.WorkerSession, id)
		if err != nil {
			return 0, false, err
		}
		if !found || status != store.ScheduleFailed {
			return id, true, nil
		}
	}
}

// implementWorkable reports whether a running story is owed a worker run now, and
// the identity of the worker instance that run would follow. A worker instance
// still running rests for its own dispatch, which continues a repair; only a
// story with no instance, or whose last instance ended, is offered, and a story
// whose runs keep ending on a fault is parked with its latest cause.
func (d *Daemon) implementWorkable(ctx context.Context, row store.ImplementStory) (int64, bool, error) {
	if row.State != store.ImplementRunning {
		return 0, false, nil
	}
	if row.WorkerSession == 0 {
		return 0, true, nil
	}
	instance, err := d.db.LatestWorkflowInstance(ctx, row.WorkerSession, string(agent.ModeImplement))
	if errors.Is(err, store.ErrNotFound) {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	switch instance.Status {
	case store.GraphRunning:
		return 0, false, nil
	case store.GraphFailed:
		failed, err := d.db.CountFailedImplementInstances(ctx, row.WorkerSession, row.StateAt)
		if err != nil {
			return 0, false, err
		}
		if failed >= maxImplementInstanceFailures {
			reason := row.LastFailure
			if reason == "" {
				reason = "The worker reached the infrastructure failure limit without a recorded diagnostic."
			}
			_, err := d.db.TransitionImplementStory(ctx, store.ImplementTransition{
				Parent: row.Parent, Generation: row.Generation, Key: row.Key,
				From: store.ImplementRunning, To: store.ImplementParked,
				ExpectedRevision: row.Revision, Reason: reason, At: instance.FinishedAt,
			})
			if errors.Is(err, store.ErrImplementConflict) {
				err = nil // Another discovery or owner transition already moved it.
			}
			return 0, false, err
		}
	}
	return instance.ID, true, nil
}

// implementWorkFor reads the story work a task owes: its own story's when it is
// the worker session of a running story, and none otherwise.
func (d *Daemon) implementWorkFor(ctx context.Context, t *task) (int64, bool, error) {
	row, err := d.db.ImplementStoryByWorkerSession(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return d.implementWork(ctx, row, t.tickAt)
}

// startImplementWork runs the story work a task owes, after its comments. The work
// is a schedule keyed by what it is, which gives the run the private identity
// every run needs and keeps a story from being started again on every tick.
func (d *Daemon) startImplementWork(ctx context.Context, t *task) error {
	id, ok, err := d.implementWorkFor(ctx, t)
	if err != nil || !ok {
		return err
	}
	attempted, err := d.db.ScheduleAttempted(ctx, t.session.ID, id)
	if err != nil || attempted {
		return err
	}
	stored, _, err := d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{
		SessionID: t.session.ID, SourceCommentID: id, OwnerHandle: d.ticketIdentities(ctx).OwnerHandle,
		Payload: implementWorkPayload, DueAt: t.tickAt.UTC(),
	})
	if err != nil {
		return err
	}
	if stored.Status != store.SchedulePending {
		return nil
	}
	reserved, ok, err := d.db.ReserveSchedule(ctx, stored.ID, t.tickAt.UTC())
	if err != nil || !ok {
		return err
	}
	scheduled := *t
	scheduled.scheduled = &reserved
	d.runScheduled(ctx, &scheduled, reserved)
	return nil
}

// implementWorkSettled reports that a schedule the factory started for story work
// found nothing left to run: the ledger moved on before the run began. Such a
// schedule is finished, not returned to pending, or it would be offered again on
// every tick.
func (d *Daemon) implementWorkSettled(ctx context.Context, t *task, scheduled store.Schedule) bool {
	if scheduled.Payload != implementWorkPayload {
		return false
	}
	id, ok, err := d.implementWorkFor(ctx, t)
	return err == nil && (!ok || id != scheduled.SourceCommentID)
}

// graphImplementStories offers every running story that has no live worker as a
// task of its own, with its own session, branch and worktree. The existing pool
// admits as many as the run limit allows and the rest are offered again on the
// next tick, so several stories build side by side and one that fails stops no
// other.
func (d *Daemon) graphImplementStories(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate, now time.Time) error {
	rows, err := d.db.ListImplementStoriesNeedingWorker(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		parent, err := d.db.GetSessionByTaskRef(ctx, row.Parent)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		q := byRepo[parent.RepoID]
		if q == nil {
			continue
		}
		stories, err := d.db.ListManagedStories(ctx, row.Parent, int(row.Generation))
		if err != nil {
			return err
		}
		number := 0
		var remoteID int64
		for _, story := range stories {
			if story.Key == row.Key {
				number = story.IssueNumber
				remoteID = story.IssueID
			}
		}
		if number < 1 {
			continue
		}
		session, err := d.managedChildSession(ctx, q.repo, row.Parent, number, remoteID)
		if err != nil {
			return fmt.Errorf("find the task of story %s: %w", row.Key, err)
		}
		if row.WorkerSession != session.ID {
			if err := d.db.SetImplementWorkerSession(ctx, row.Parent, row.Generation, row.Key, session.ID, now.UTC()); err != nil {
				if errors.Is(err, store.ErrImplementConflict) {
					continue
				}
				return err
			}
			row.WorkerSession = session.ID
		}
		id, ok, err := d.implementWork(ctx, row, now)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		attempted, err := d.db.ScheduleAttempted(ctx, session.ID, id)
		if err != nil {
			return err
		}
		if attempted {
			continue
		}
		if prior := seen[session.ID]; prior != nil {
			prior.addSource("implement_story")
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return err
		}
		c := q.candidate(session, ref)
		c.addSource("implement_story")
		q.candidates = append(q.candidates, c)
		seen[session.ID] = c
	}
	return nil
}

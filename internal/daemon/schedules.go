package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// dispatchSchedules offers due schedules before ordinary comment candidates.
// A schedule is factory-owned durable work, not a delayed comment: it is
// selected in UTC due-time/id order on every tick, reserved immediately before
// the worker starts, and otherwise remains pending for a later eligible poll.
func (d *Daemon) dispatchSchedules(ctx context.Context, repos map[int64]store.Repo, tickAt time.Time, result *TickResult) error {
	// Asking for more rows than there are slots is intentional. A row for an
	// already-in-flight session is skipped without taking a slot; inspecting the
	// rest lets a different session use the capacity this tick.
	schedules, err := d.db.ListEligibleSchedules(ctx, tickAt.UTC(), MaxConcurrentRuns)
	if err != nil {
		return fmt.Errorf("list due scheduled work: %w", err)
	}
	for _, scheduled := range schedules {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.offerSchedule(ctx, repos, scheduled, tickAt, result) {
			// The pool is shared with ordinary work. Once it is full there is no
			// queue to fill; later due schedules remain pending for the next poll.
			break
		}
	}
	return nil
}

// offerSchedule reserves and starts one due schedule. Its boolean says whether
// the dispatch pass may inspect another row; false means the shared capacity is
// full, not that this individual row was invalid or already in flight.
func (d *Daemon) offerSchedule(ctx context.Context, repos map[int64]store.Repo, scheduled store.Schedule, tickAt time.Time, result *TickResult) bool {
	workerCtx := ctx
	ctx, cancel := d.tickPhaseContext(ctx)
	defer cancel()
	ctx = sessionContext(ctx, scheduled.SessionID)
	session, err := d.db.GetSession(ctx, scheduled.SessionID)
	if err != nil {
		d.log.ErrorContext(ctx, "could not read the session for scheduled work; it remains pending",
			slog.Int64("schedule_id", scheduled.ID), slog.Any("error", err))
		return true
	}
	repo, ok := repos[session.RepoID]
	if !ok || repo.Forge != store.ForgeGitHub {
		d.log.WarnContext(ctx, "scheduled work belongs to a repository this daemon cannot poll; it remains pending",
			slog.Int64("schedule_id", scheduled.ID), slog.Int64("session_id", session.ID))
		return true
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		d.log.ErrorContext(ctx, "scheduled work has an invalid task reference; it remains pending",
			slog.Int64("schedule_id", scheduled.ID), slog.Any("error", err))
		return true
	}
	log := d.log.With(
		slog.String("repo", repo.Owner+"/"+repo.Name),
		slog.String("task", ref.String()),
		slog.Int64("session_id", session.ID),
		slog.Int64("schedule_id", scheduled.ID))
	if d.observeCandidateHold(ctx, &candidate{session: session, ref: ref}, tickAt) {
		return true
	}
	if !d.enterDoctorGuarded(ctx, session.ID) {
		log.DebugContext(ctx, "scheduled work waits for this task's in-flight turn")
		return true
	}
	if !d.pool.try() {
		d.inflight.leave(session.ID)
		result.Deferred++
		return false
	}

	t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, log: log, tickAt: tickAt}
	issue, err := d.readScheduledIssue(ctx, t)
	if err != nil || issue.State != github.StateOpen {
		d.pool.done()
		d.inflight.leave(session.ID)
		if err != nil {
			d.rateLimited(ctx, log, tickAt, err)
			d.prepareFailed(ctx, t, err)
			log.ErrorContext(ctx, "could not read the issue for scheduled work; it remains pending", slog.Any("error", err))
		} else {
			log.InfoContext(ctx, "scheduled work is not started because its issue is closed")
		}
		return true
	}
	if _, held := d.connectionHold(ctx, tickAt); held {
		d.pool.done()
		d.inflight.leave(session.ID)
		result.Deferred++
		return d.connections != nil
	}
	reserved, ok, err := d.db.ReserveSchedule(ctx, scheduled.ID, tickAt.UTC())
	if err != nil {
		d.pool.done()
		d.inflight.leave(session.ID)
		log.ErrorContext(ctx, "could not reserve scheduled work; it remains pending", slog.Any("error", err))
		return true
	}
	if !ok {
		d.pool.done()
		d.inflight.leave(session.ID)
		return true
	}
	t.scheduled = &reserved
	t.issue = issue
	t.workerLog = t.log.With(slog.Any("dispatch_sources", []string{"due_schedule"}), slog.Time("tick_at", tickAt))
	t.workerLog.DebugContext(ctx, "worker dispatched")
	go d.runDoctorGuardedWorker(sessionContext(workerCtx, scheduled.SessionID), t, func(ctx context.Context) { d.runScheduled(ctx, t, reserved) })
	result.Dispatched++
	return true
}

// readScheduledIssue keeps scheduled ticket routing and code preparation pinned
// to the session, as ordinary materialisation does.
func (d *Daemon) readScheduledIssue(ctx context.Context, t *task) (github.Issue, error) {
	if d.connections != nil {
		snapshot, err := d.db.GetExecutionSnapshot(ctx, t.session.ID)
		if err != nil {
			return github.Issue{}, err
		}
		t.repo = snapshot.CodeTarget
		binding, err := d.db.GetSessionTicket(ctx, t.session.ID)
		if err != nil {
			return github.Issue{}, err
		}
		owner, name, _ := strings.Cut(binding.Locator.Container, "/")
		t.forgeRepo = github.Repo{Owner: owner, Name: name}
	}
	return d.readIssue(ctx, t.forgeRepo, t.session.IssueNumber)
}

// runScheduled delivers the stored payload, never the /schedule control, to
// the normal turn path. SourceCommentID remains the report anchor; TriggerID
// is private schedule identity so each proposal item gets its own run.
func (d *Daemon) runScheduled(ctx context.Context, t *task, scheduled store.Schedule) {
	t.implementCoordinatorHandedOff = false
	command := turn.ParseCommand(scheduled.Payload)
	trigger := turn.Trigger{CommentID: scheduled.TriggerID(), Body: scheduled.Payload, Author: scheduled.OwnerHandle}
	err := d.run(ctx, t, trigger, command, t.log)
	if err != nil {
		t.recordWorkerError(err)
	}
	run, exists, lookupErr := d.db.GetRunByTrigger(ctx, t.session.ID, scheduled.TriggerID())
	if lookupErr != nil {
		t.recordWorkerError(lookupErr)
		t.log.ErrorContext(ctx, "could not read the scheduled work outcome", slog.Any("error", lookupErr))
		return
	}
	if !exists {
		// A cross-process session lock is the only ordinary no-run result. Put
		// the reservation back so another poll can retry it. Any other no-run
		// error is terminal: no agent has run, but silently retrying a malformed
		// payload forever would make the durable queue permanently stuck.
		if err == nil && (d.planWorkSettled(ctx, t, scheduled) || d.implementWorkSettled(ctx, t, scheduled)) {
			// Work whose state moved on or is explicitly held has nothing to run.
			d.finishScheduled(ctx, t, scheduled, store.ScheduleSucceeded, 0)
			return
		}
		if err == nil {
			t.recordWorkerStatus(workerDeferred)
			if _, requeueErr := d.db.RequeueSchedule(ctx, scheduled.ID, t.tickAt.UTC()); requeueErr != nil {
				t.recordWorkerError(requeueErr)
				t.log.ErrorContext(ctx, "could not return lock-contended scheduled work to pending", slog.Any("error", requeueErr))
			}
			return
		}
		d.finishScheduled(ctx, t, scheduled, store.ScheduleFailed, 0)
		return
	}
	if !run.Reportable() {
		t.recordWorkerStatus(workerDeferred)
		if run.Status == store.RunPrelaunch {
			if _, requeueErr := d.db.RequeueSchedule(ctx, scheduled.ID, t.tickAt.UTC()); requeueErr != nil {
				t.recordWorkerError(requeueErr)
				t.log.ErrorContext(ctx, "could not requeue prelaunch scheduled preparation", slog.Any("error", requeueErr))
			}
		}
		return
	}
	// A quota chain retains the original prerequisite until its final outcome.
	current, lookupErr := d.db.GetSchedule(ctx, scheduled.ID)
	if lookupErr != nil {
		t.recordWorkerError(lookupErr)
		t.log.ErrorContext(ctx, "read scheduled recovery state", slog.Any("error", lookupErr))
		return
	}
	if current.Status == "suspended" {
		t.recordWorkerStatus(workerDeferred)
		return
	}
	status := store.ScheduleFailed
	if run.Succeeded() {
		status = store.ScheduleSucceeded
	}
	completed := *t
	completed.tickAt = run.FinishedAt
	d.finishScheduled(ctx, &completed, scheduled, status, run.ID)
}

func (d *Daemon) finishScheduled(ctx context.Context, t *task, scheduled store.Schedule, status store.ScheduleStatus, runID int64) {
	if _, err := d.db.FinishSchedule(ctx, scheduled.ID, status, runID, t.tickAt.UTC()); err != nil && !errors.Is(err, store.ErrNotFound) {
		t.recordWorkerError(err)
		t.log.ErrorContext(ctx, "could not record the terminal outcome of scheduled work", slog.Any("error", err))
		return
	}
	if status == store.ScheduleSucceeded {
		d.prepFailures.clear(t.session.ID)
	}
	if status == store.ScheduleFailed {
		d.replyBlockedSchedules(ctx, t, scheduled.ID)
	}
	t.log.InfoContext(ctx, "scheduled work complete", slog.String("status", string(status)), slog.Int64("run_id", runID))
}

func (d *Daemon) replyBlockedSchedules(ctx context.Context, t *task, failedID int64) {
	blocked, err := d.db.ListUnreportedBlockedDescendants(ctx, failedID)
	if err != nil {
		t.recordWorkerError(err)
		t.log.ErrorContext(ctx, "could not list schedules blocked by failed work", slog.Any("error", err))
		return
	}
	for _, dependent := range blocked {
		posted, postErr := d.reportReply(ctx, turn.ReplyRequest{
			Ref: t.ref, SessionID: t.session.ID, CommentID: dependent.SourceCommentID,
			ScheduleKind: report.ReplyScheduleBlocked, Schedule: report.ScheduleReply{ID: dependent.ID},
		})
		if postErr != nil {
			if isRecoveryPending(postErr) {
				t.recordWorkerStatus(workerDeferred)
				continue
			}
			t.recordWorkerError(postErr)
			t.log.ErrorContext(ctx, "could not report blocked scheduled work; a later failure path retries it",
				slog.Int64("blocked_schedule_id", dependent.ID), slog.Any("error", postErr))
			continue
		}
		if posted.Skipped {
			continue
		}
		if err := d.db.MarkScheduleBlockedReply(ctx, dependent.ID, t.tickAt.UTC()); err != nil {
			t.recordWorkerError(err)
			t.log.ErrorContext(ctx, "blocked scheduled work reply was posted but not recorded; it may be posted again",
				slog.Int64("blocked_schedule_id", dependent.ID), slog.Any("error", err))
		}
	}
}

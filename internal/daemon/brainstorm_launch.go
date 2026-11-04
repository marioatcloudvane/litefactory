package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// brainstormLaunchBatch bounds the items one owner-action pass inspects.
const brainstormLaunchBatch = 16

// brainstormStartWait bounds how long an admitted launch, or a Plan already
// running on the issue, may go without its brief seed and first agent before
// the item counts as created but not started.
const brainstormStartWait = 15 * time.Minute

// brainstormLaunchCommand is what a launch attempt hands to the normal locked
// turn path. It is never posted on the forge.
const brainstormLaunchCommand = "/plan Start Plan from the brainstorm brief."

// stepBrainstormLaunches starts Plan for created brainstorm items: one launch
// attempt at a time per item, each with its own private run identity, handed
// to the normal turn path like scheduled work and classified from its run row.
func (d *Daemon) stepBrainstormLaunches(ctx context.Context) {
	items, err := d.db.ActiveBrainstormTaskItems(ctx, brainstormLaunchBatch)
	if err != nil {
		d.log.ErrorContext(ctx, "could not list brainstorm items to start", slog.Any("error", err))
		return
	}
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		if it.State != store.BrainstormItemCreated && it.State != store.BrainstormItemPlanStarting {
			continue
		}
		if err := d.stepBrainstormLaunch(ctx, it); err != nil {
			d.log.ErrorContext(ctx, "brainstorm Plan start step failed; it is retried on the next pass",
				slog.String("item", it.ID), slog.Any("error", err))
		}
	}
}

func (d *Daemon) brainstormNow() time.Time {
	// The launch step runs in the owner-action loop, outside Tick, so it reads
	// wall time rather than the daemon's tick clock.
	return d.brainstormTasks.wallNow().UTC()
}

// stepBrainstormLaunch advances one item by at most one hand-off.
func (d *Daemon) stepBrainstormLaunch(ctx context.Context, it store.BrainstormTaskItem) error {
	now := d.brainstormNow()
	batch, err := d.db.ReadBrainstormTaskBatch(ctx, it.BatchID)
	if err != nil {
		return err
	}
	repo, err := d.db.GetRepo(ctx, batch.RepositoryID)
	if err != nil {
		return err
	}
	if it.State == store.BrainstormItemCreated {
		if it.IssueNumber < 1 {
			return errors.New("created brainstorm item has no issue")
		}
		session, err := d.db.GetOrCreateSession(ctx, repo, it.IssueNumber)
		if err != nil {
			return err
		}
		next := it
		next.SessionID = session.ID
		next.State = store.BrainstormItemPlanStarting
		ok, err := d.db.SaveBrainstormTaskItem(ctx, next, store.BrainstormItemCreated, now)
		if err != nil || !ok {
			return err
		}
		next.UpdatedAt = now
		it = next
	}
	attempts, err := d.db.BrainstormLaunchAttempts(ctx, it.ID)
	if err != nil {
		return err
	}
	var open *store.BrainstormLaunchAttempt
	for i := range attempts {
		a := attempts[i]
		if a.Open() {
			open = &a
		}
		if a.State == store.BrainstormLaunchSucceeded {
			// A launch that was admitted owns the item from here; it never
			// gets a second active launch, only a check that it really started.
			return d.settleBrainstormStart(ctx, it, a, now)
		}
	}
	if open == nil {
		if it.FailedLaunchAttempts >= store.BrainstormLaunchFailureLimit {
			return d.failBrainstormStart(ctx, it, it.FailedLaunchAttempts, now)
		}
		latest, err := d.db.LatestWorkflowInstance(ctx, it.SessionID, "plan")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && latest.Status == store.GraphRunning {
			// Plan is already running or waiting for the owner on this issue:
			// one active Plan launch per session, so no attempt opens. Only
			// this item's own admitted launch carries its brief seed, so that
			// Plan does not count as started for it.
			next := it
			if now.Sub(it.UpdatedAt) >= brainstormStartWait {
				next.State = store.BrainstormItemPlanStartFailed
				next.Reason = fmt.Sprintf("created as issue #%d but not started; another Plan is running on the issue without this brief", it.IssueNumber)
			} else {
				next.Reason = "created; another Plan is running on the issue without this brief"
				if next.Reason == it.Reason {
					return nil
				}
			}
			_, err = d.db.SaveBrainstormTaskItem(ctx, next, it.State, now)
			return err
		}
		// A failed or finished Plan instance is followed through the
		// successor link AdmitWorkflowLaunch already keeps.
		a, _, err := d.db.OpenBrainstormLaunchAttempt(ctx, it.ID, it.SessionID, now)
		if err != nil {
			return err
		}
		open = &a
	}
	if settled, err := d.boundOpenBrainstormAttempt(ctx, it, *open, now); err != nil || settled {
		return err
	}
	return d.driveBrainstormAttempt(ctx, repo, it, *open)
}

// boundOpenBrainstormAttempt keeps an open attempt within the start wait
// before it is handed off again. An admitted launch whose turn is still under
// way is judged by its seed and first agent now, not when the turn ends. An
// attempt that never got admitted (deferred, prelaunch or lock-held each pass)
// settles as created but not started once the wait has passed. The attempt
// stays open, so a later owner retry continues the same launch and no second
// one opens.
func (d *Daemon) boundOpenBrainstormAttempt(ctx context.Context, it store.BrainstormTaskItem, a store.BrainstormLaunchAttempt, now time.Time) (bool, error) {
	since := a.CreatedAt
	if it.UpdatedAt.After(since) {
		since = it.UpdatedAt
	}
	state, err := d.db.BrainstormLaunchState(ctx, a.SessionID, a.TriggerID())
	if err != nil {
		return false, err
	}
	if state.Admitted {
		seeded := state.HasSeed && (state.Seed.State == store.BrainstormSeedComplete || state.Seed.State == store.BrainstormSeedSkippedExisting)
		ended, err := d.brainstormPlanEnded(ctx, state)
		if err != nil {
			return false, err
		}
		if (!seeded || !state.AgentDispatched) && !ended && now.Sub(since) < brainstormStartWait {
			// Saving a waiting reason here would restart the wait.
			return false, nil
		}
		timed := a
		timed.UpdatedAt = since
		if err = d.settleBrainstormStart(ctx, it, timed, now); err != nil {
			return false, err
		}
		current, err := d.db.ReadBrainstormTaskItem(ctx, it.ID)
		if err != nil {
			return false, err
		}
		return current.State != store.BrainstormItemPlanStarting, nil
	}
	if now.Sub(since) < brainstormStartWait {
		return false, nil
	}
	next := it
	next.State = store.BrainstormItemPlanStartFailed
	next.Reason = fmt.Sprintf("created as issue #%d but not started; Plan could not be launched within %s", it.IssueNumber, brainstormStartWait)
	_, err = d.db.SaveBrainstormTaskItem(ctx, next, it.State, now)
	return true, err
}

func (d *Daemon) driveBrainstormAttempt(ctx context.Context, repo store.Repo, it store.BrainstormTaskItem, a store.BrainstormLaunchAttempt) error {
	session, err := d.db.GetSession(ctx, a.SessionID)
	if err != nil {
		return err
	}
	if !d.enterDoctorGuarded(ctx, session.ID) {
		// The task's turn, possibly this attempt's own run, is under way.
		return nil
	}
	handedOff := false
	defer func() {
		if !handedOff {
			d.inflight.leave(session.ID)
		}
	}()
	if a.State != store.BrainstormLaunchPending {
		// Found reserved or running with no worker: a restart. Classify it
		// from its run row, never hand it off blindly.
		_, err = d.classifyBrainstormAttempt(ctx, it, a, nil, d.brainstormNow())
		return err
	}
	// Replay: a reportable run for this identity already exists.
	if run, exists, err := d.db.GetRunByTrigger(ctx, session.ID, a.TriggerID()); err != nil {
		return err
	} else if exists && run.Reportable() {
		_, err = d.classifyBrainstormAttempt(ctx, it, a, nil, d.brainstormNow())
		return err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return err
	}
	reservation, reason := d.reserveInput()
	if reservation == nil {
		d.log.InfoContext(ctx, "brainstorm Plan start deferred for capacity", slog.String("reason", reason))
		return nil
	}
	tickAt := d.brainstormNow()
	log := d.log.With(slog.String("task", ref.String()), slog.Int64("session_id", session.ID), slog.Int64("brainstorm_launch_attempt", a.ID))
	t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, log: log, tickAt: tickAt}
	t.workerLog = log.With(slog.Any("dispatch_sources", []string{"brainstorm_launch"}), slog.Time("tick_at", tickAt))
	issue, err := d.readScheduledIssue(ctx, t)
	if err != nil {
		reservation.abandon()
		return err
	}
	if issue.State != github.StateOpen {
		reservation.abandon()
		if _, err = d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, store.BrainstormLaunchPending, store.BrainstormLaunchReserved, "", 0, "", tickAt); err != nil {
			return err
		}
		a.State = store.BrainstormLaunchReserved
		return d.closeBrainstormAttemptFailed(ctx, it.ID, a, 0, "the issue is closed", tickAt)
	}
	ok, err := d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, store.BrainstormLaunchPending, store.BrainstormLaunchReserved, "", 0, "", tickAt)
	if err != nil || !ok {
		reservation.abandon()
		return err
	}
	a.State = store.BrainstormLaunchReserved
	t.capacity = reservation
	t.issue = issue
	d.inflight.label(session.ID, "brainstorm_launch")
	handedOff = true
	go d.runDoctorGuardedWorker(sessionContext(context.WithoutCancel(ctx), session.ID), t, func(ctx context.Context) {
		d.runBrainstormLaunch(ctx, t, it, a)
	})
	return nil
}

// brainstormLaunchTrigger is the private, owner-authored trigger of one
// attempt. Its body is the command only; the issue body stays bot-authored and
// is never run as an instruction.
func (d *Daemon) brainstormLaunchTrigger(ctx context.Context, a store.BrainstormLaunchAttempt) turn.Trigger {
	ids := d.ticketIdentities(ctx)
	return turn.Trigger{CommentID: a.TriggerID(), Body: brainstormLaunchCommand, Author: ids.OwnerHandle, AuthorID: ids.OwnerUserID}
}

func (d *Daemon) runBrainstormLaunch(ctx context.Context, t *task, it store.BrainstormTaskItem, a store.BrainstormLaunchAttempt) {
	now := d.brainstormNow()
	if ok, err := d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, store.BrainstormLaunchReserved, store.BrainstormLaunchRunning, "", 0, "", now); err != nil || !ok {
		if err != nil {
			t.recordWorkerError(err)
		}
		return
	}
	a.State = store.BrainstormLaunchRunning
	trigger := d.brainstormLaunchTrigger(ctx, a)
	// Preparation failures of the launch return their cause instead of being
	// held as an owner comment, so a terminal failure is told from a deferral.
	t.commentUnderWay = trigger.CommentID
	runErr := d.run(ctx, t, trigger, turn.ParseCommand(trigger.Body), t.log)
	t.commentUnderWay = 0
	if runErr != nil {
		t.recordWorkerError(runErr)
	}
	// After the hand-off, so a note still pending in report delivery can
	// never hold the launch run back.
	if run, exists, err := d.db.GetRunByTrigger(ctx, t.session.ID, a.TriggerID()); err == nil && exists && run.Reportable() {
		d.postBrainstormLaunchNote(ctx, t, a)
	}
	if _, err := d.classifyBrainstormAttempt(ctx, it, a, &runErr, d.brainstormNow()); err != nil {
		t.recordWorkerError(err)
		t.log.ErrorContext(ctx, "could not classify the brainstorm Plan start", slog.Any("error", err))
	}
}

// classifyBrainstormAttempt reads the attempt's run and settles it one way
// only: replay, prelaunch deferral, terminal failure or started. handoff is
// nil when no hand-off happened in this process (replay or restart).
func (d *Daemon) classifyBrainstormAttempt(ctx context.Context, it store.BrainstormTaskItem, a store.BrainstormLaunchAttempt, handoff *error, now time.Time) (string, error) {
	run, exists, err := d.db.GetRunByTrigger(ctx, a.SessionID, a.TriggerID())
	if err != nil {
		return "", err
	}
	if !exists {
		if handoff != nil && *handoff != nil && !errors.Is(*handoff, context.Canceled) {
			// The hand-off failed before any run row: waiting will not help.
			return store.BrainstormLaunchOutcomeFailed, d.closeBrainstormAttemptFailed(ctx, it.ID, a, 0, (*handoff).Error(), now)
		}
		// The task lock was held, or a restart found no run: same identity
		// back to pending, not counted as a retry.
		_, err = d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, a.State, store.BrainstormLaunchPending, store.BrainstormLaunchOutcomeDeferred, 0, "", now)
		return store.BrainstormLaunchOutcomeDeferred, err
	}
	if !run.Reportable() {
		if run.Status == store.RunPrelaunch {
			_, err = d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, a.State, store.BrainstormLaunchPending, store.BrainstormLaunchOutcomeDeferred, run.ID, "", now)
			return store.BrainstormLaunchOutcomeDeferred, err
		}
		// Still running: the run's own recovery owns it; classify later.
		return "", nil
	}
	state, err := d.db.BrainstormLaunchState(ctx, a.SessionID, a.TriggerID())
	if err != nil {
		return "", err
	}
	outcome := store.BrainstormLaunchOutcomeStarted
	if handoff == nil {
		outcome = store.BrainstormLaunchOutcomeReplayed
	}
	if state.Admitted {
		if _, err = d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, a.State, store.BrainstormLaunchSucceeded, outcome, run.ID, "", now); err != nil {
			return "", err
		}
		a.State, a.Outcome, a.RunID, a.UpdatedAt = store.BrainstormLaunchSucceeded, outcome, run.ID, now
		current, err := d.db.ReadBrainstormTaskItem(ctx, it.ID)
		if err != nil {
			return "", err
		}
		return outcome, d.settleBrainstormStart(ctx, current, a, now)
	}
	reason := "Plan did not start"
	if run.Error != "" {
		reason = run.Error
	}
	return store.BrainstormLaunchOutcomeFailed, d.closeBrainstormAttemptFailed(ctx, it.ID, a, run.ID, reason, now)
}

// closeBrainstormAttemptFailed closes a terminal failure and counts it on the
// item; only such a failure lets a new attempt, with a new identity, open.
func (d *Daemon) closeBrainstormAttemptFailed(ctx context.Context, itemID string, a store.BrainstormLaunchAttempt, runID int64, reason string, now time.Time) error {
	moved, err := d.db.MoveBrainstormLaunchAttempt(ctx, a.ID, a.State, store.BrainstormLaunchFailed, store.BrainstormLaunchOutcomeFailed, runID, reason, now)
	if err != nil || !moved {
		return err
	}
	current, err := d.db.ReadBrainstormTaskItem(ctx, itemID)
	if err != nil {
		return err
	}
	next := current
	next.FailedLaunchAttempts++
	next.Reason = reason
	_, err = d.db.SaveBrainstormTaskItem(ctx, next, current.State, now)
	return err
}

// settleBrainstormStart marks the item started only once its brief seed is
// complete (or an existing brief was kept) and the first Plan agent was
// dispatched. A launch whose Plan ends first, or whose brief and first agent
// are not ready within brainstormStartWait, leaves the item created but not
// started, with the reason, on the same issue and launch.
func (d *Daemon) settleBrainstormStart(ctx context.Context, it store.BrainstormTaskItem, a store.BrainstormLaunchAttempt, now time.Time) error {
	if it.State != store.BrainstormItemPlanStarting {
		return nil
	}
	state, err := d.db.BrainstormLaunchState(ctx, a.SessionID, a.TriggerID())
	if err != nil {
		return err
	}
	seeded := state.HasSeed && (state.Seed.State == store.BrainstormSeedComplete || state.Seed.State == store.BrainstormSeedSkippedExisting)
	next := it
	if seeded && state.AgentDispatched {
		next.State = store.BrainstormItemPlanStarted
		next.Reason = ""
		if state.Seed.State == store.BrainstormSeedSkippedExisting {
			next.Reason = "Plan uses the brief already on the issue's branch"
		}
	} else {
		reason := "created; Plan is admitted and its brief is not ready yet"
		if !state.HasSeed {
			reason = "created; Plan is admitted without a brief"
		}
		ended, err := d.brainstormPlanEnded(ctx, state)
		if err != nil {
			return err
		}
		// The wait runs from admission or from the item's last change,
		// whichever is later, so an owner retry gets a fresh wait on the same
		// issue and launch. The item stays final afterwards; a retry re-checks.
		since := a.UpdatedAt
		if it.UpdatedAt.After(since) {
			since = it.UpdatedAt
		}
		switch {
		case ended:
			next.State = store.BrainstormItemPlanStartFailed
			reason = fmt.Sprintf("created as issue #%d but not started; Plan ended before its brief and first agent were ready", it.IssueNumber)
		case now.Sub(since) >= brainstormStartWait:
			next.State = store.BrainstormItemPlanStartFailed
			reason = fmt.Sprintf("created as issue #%d but not started; its brief and first agent were not ready within %s", it.IssueNumber, brainstormStartWait)
		case next.Reason == reason:
			return nil
		}
		next.Reason = reason
	}
	_, err = d.db.SaveBrainstormTaskItem(ctx, next, it.State, now)
	return err
}

// brainstormPlanEnded reports whether the admitted launch's Plan instance has
// failed or finished.
func (d *Daemon) brainstormPlanEnded(ctx context.Context, state store.BrainstormLaunchStatus) (bool, error) {
	if !state.Admitted || state.InstanceID == 0 {
		return false, nil
	}
	instance, err := d.db.GetGraphInstance(ctx, state.InstanceID)
	if err != nil {
		return false, err
	}
	return instance.Status != store.GraphRunning && instance.Status != store.GraphPending, nil
}

func (d *Daemon) failBrainstormStart(ctx context.Context, it store.BrainstormTaskItem, failed int, now time.Time) error {
	next := it
	next.State = store.BrainstormItemPlanStartFailed
	next.Reason = fmt.Sprintf("created as issue #%d; Plan did not start after %d attempts", it.IssueNumber, failed)
	_, err := d.db.SaveBrainstormTaskItem(ctx, next, it.State, now)
	return err
}

type brainstormLaunchNoteReporter interface {
	ReportBrainstormLaunchNote(context.Context, naming.Ref, int64, int64) (turn.Posted, error)
}

// postBrainstormLaunchNote posts the factory note on the new issue. A failure
// stays pending in report delivery and never blocks Plan.
func (d *Daemon) postBrainstormLaunchNote(ctx context.Context, t *task, a store.BrainstormLaunchAttempt) {
	r, ok := d.reporter.(brainstormLaunchNoteReporter)
	if !ok {
		return
	}
	source := store.ReportRecovery{Key: recoveryKey(t.session.ID, a.TriggerID()) + ":brainstorm-note", SessionID: t.session.ID, TriggerID: a.TriggerID(), Task: t.ref.String()}
	if _, err := d.reportAttempt(ctx, source, func() (turn.Posted, error) {
		return r.ReportBrainstormLaunchNote(ctx, t.ref, t.session.ID, a.TriggerID())
	}); err != nil {
		t.log.WarnContext(ctx, "the brainstorm factory note is not posted yet; Plan starts anyway", slog.Any("error", err))
	}
}

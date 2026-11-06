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

// bugStartBatch bounds the filings one start pass inspects.
const bugStartBatch = 100

// bugStartWait bounds how long an admitted Bug may go without an analyst
// attempt, counted from the admission or the latest run after it.
const bugStartWait = 15 * time.Minute

// bugStartAnalystNode is the Bug graph node whose attempt proves dispatch.
const bugStartAnalystNode = store.BugStartAnalystNode

// bugStartBody is the private start trigger's body: a plain sentence, never a
// mode command and never posted on the forge.
const bugStartBody = "Start the Bug analysis of this issue filed for the owner from a chat client."

// The partial texts a filing shows while it waits for the analyst.
const (
	bugStartNotStarted = "labelled but not yet started"
	bugStartAdmitted   = "labelled; Bug admitted, analysis not yet dispatched"
	bugStartRecorded   = "labelled; Bug run recorded but not started; reply run it again on the issue to continue"
	bugStartParked     = "Bug analysis stopped before the analyst; reply on the issue to resume it"
)

// bugStartHandOff and bugStartAuthority are the step's effect seams: the real
// hand-off to the locked turn path and the owner-action effect check.
var (
	bugStartHandOff   = (*Daemon).handOffBugStart
	bugStartAuthority = (*Daemon).bugFilingAuthority
)

// bugStartPhase is the start state judged from retained rows only.
type bugStartPhase int

const (
	// bugStartS0: no standalone Bug instance on the session.
	bugStartS0 bugStartPhase = iota
	// bugStartS1: the runtime's untouched-launch shape, no run at all.
	bugStartS1
	// bugStartS2r: the store's stopped-start state (StoppedBugLaunch).
	bugStartS2r
	// bugStartS2: a live run, dispatch, attempt or claimed reply, or another
	// pre-analyst shape the Bug runtime's Reconcile owns.
	bugStartS2
	// bugStartS3: an analyst attempt is recorded.
	bugStartS3
)

// bugStartPhaseOf classifies the evidence. S2r is exactly the store's
// stopped-start rule (ev.Stopped), the state the Bug runtime's restart check
// accepts from a fresh owner reply, so the reply text is never shown where
// the reply would be refused.
func bugStartPhaseOf(ev store.BugStartEvidence) bugStartPhase {
	if !ev.Admitted {
		return bugStartS0
	}
	for _, a := range ev.Attempts {
		if a.Node == bugStartAnalystNode {
			return bugStartS3
		}
	}
	if len(ev.Runs) == 0 && len(ev.Attempts) == 0 && len(ev.Transitions) == 0 && !ev.HasDispatch && !ev.HasWait && !ev.HasComment {
		return bugStartS1
	}
	if ev.Stopped {
		return bugStartS2r
	}
	return bugStartS2
}

// stepBugFilingStarts starts Bug for labelled bug filings: one private start
// attempt at a time per filing, handed to the normal turn path like the
// brainstorm launch, and settled only on retained analyst evidence.
func (d *Daemon) stepBugFilingStarts(ctx context.Context) {
	filings, err := d.db.ActiveBugFilings(ctx, bugStartBatch)
	if err != nil {
		d.log.ErrorContext(ctx, "could not list bug filings to start", slog.Any("error", err))
		return
	}
	for _, f := range filings {
		if ctx.Err() != nil {
			return
		}
		if f.State != store.BugFilingLabelled && f.State != store.BugFilingStartAdmitted {
			continue
		}
		if err := d.stepBugFilingStart(ctx, f); err != nil {
			d.log.ErrorContext(ctx, "bug filing start step failed; it is retried on the next pass",
				slog.Int64("filing", f.ID), slog.Any("error", err))
		}
	}
}

// stepBugFilingStart advances one filing by at most one hand-off.
func (d *Daemon) stepBugFilingStart(ctx context.Context, f store.BugFiling) error {
	if f.State != store.BugFilingLabelled && f.State != store.BugFilingStartAdmitted {
		return nil
	}
	now := d.brainstormNow()
	repo, err := d.db.GetRepo(ctx, f.RepositoryID)
	if err != nil {
		return err
	}
	if f.SessionID == 0 {
		if f.IssueNumber < 1 {
			return errors.New("labelled bug filing has no issue")
		}
		var session store.Session
		if f.Tracker == store.BugTrackerLinear {
			session, err = d.linearBugSession(ctx, repo, f)
		} else {
			session, err = d.db.GetOrCreateSession(ctx, repo, f.IssueNumber)
		}
		if err != nil {
			return err
		}
		next := f
		next.SessionID = session.ID
		if next.Reason == "" && next.State == store.BugFilingLabelled {
			next.Reason = bugStartNotStarted
		}
		ok, err := d.db.SaveBugFiling(ctx, next, f.State, now)
		if err != nil || !ok {
			return err
		}
		next.UpdatedAt = now
		f = next
	}
	ev, err := d.db.BugStartEvidence(ctx, f.SessionID)
	if err != nil {
		return err
	}
	attempts, err := d.db.BugFilingStartAttempts(ctx, f.ID)
	if err != nil {
		return err
	}
	var open *store.BugFilingStartAttempt
	for i := range attempts {
		if attempts[i].Open() {
			a := attempts[i]
			open = &a
		}
	}
	if ev.Admitted {
		// The session has its Bug: no further hand-off ever, whichever
		// trigger admitted it. An attempt left reserved or running with no
		// worker is still settled from its run row.
		if open != nil && open.State != store.BugStartPending && d.enterDoctorGuarded(ctx, f.SessionID) {
			err = d.classifyBugStartAttempt(ctx, f, *open, nil, now)
			d.inflight.leave(f.SessionID)
			if err != nil {
				return err
			}
			if f, err = d.db.ReadBugFiling(ctx, f.ID); err != nil {
				return err
			}
		}
		return d.settleBugStart(ctx, f, ev, now)
	}
	if open == nil {
		if f.FailedStartAttempts >= store.BugStartFailureLimit {
			return d.failBugStart(ctx, f, bugStartLimitReason(f), now)
		}
		a, _, err := d.db.OpenBugFilingStartAttempt(ctx, f.ID, f.SessionID, now)
		if err != nil {
			return err
		}
		open = &a
	}
	return d.driveBugStartAttempt(ctx, repo, f, *open)
}

func bugStartLimitReason(f store.BugFiling) string {
	return fmt.Sprintf("filed and labelled as %s but not started; an owner comment on the issue starts it", f.IssueLink)
}

func (d *Daemon) driveBugStartAttempt(ctx context.Context, repo store.Repo, f store.BugFiling, a store.BugFilingStartAttempt) error {
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
	if a.State != store.BugStartPending {
		// Found reserved or running with no worker: a restart. Classify it
		// from its run row, never hand it off blindly.
		err = d.classifyBugStartAttempt(ctx, f, a, nil, d.brainstormNow())
		return err
	}
	// A run already recorded for this identity is classified, not repeated.
	// Only a prelaunch run, which never crossed the launch boundary, may be
	// handed off again under the same identity.
	if run, exists, err := d.db.GetRunByTrigger(ctx, session.ID, a.TriggerID()); err != nil {
		return err
	} else if exists && run.Status != store.RunPrelaunch {
		err = d.classifyBugStartAttempt(ctx, f, a, nil, d.brainstormNow())
		return err
	}
	if err := bugStartAuthority(d, ctx, f); err != nil {
		if errors.Is(err, errBugFilingRevoked) {
			return d.failBugStart(ctx, f, fmt.Sprintf("authority was revoked; filed and labelled as %s, not started; an owner comment on the issue starts it", f.IssueLink), d.brainstormNow())
		}
		return err
	}
	handedOff, err = bugStartHandOff(d, ctx, repo, session, f, a)
	return err
}

// handOffBugStart reserves capacity and the attempt, then hands the private
// trigger to the doctor-guarded worker. It reports whether a worker now owns
// the session's in-flight mark.
func (d *Daemon) handOffBugStart(ctx context.Context, repo store.Repo, session store.Session, f store.BugFiling, a store.BugFilingStartAttempt) (bool, error) {
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return false, err
	}
	reservation, reason := d.reserveInput()
	if reservation == nil {
		d.log.InfoContext(ctx, "bug filing start deferred for capacity", slog.String("reason", reason))
		return false, nil
	}
	tickAt := d.brainstormNow()
	log := d.log.With(slog.String("task", ref.String()), slog.Int64("session_id", session.ID), slog.Int64("bug_start_attempt", a.ID))
	t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, log: log, tickAt: tickAt}
	t.workerLog = log.With(slog.Any("dispatch_sources", []string{"bug_filing_start"}), slog.Time("tick_at", tickAt))
	issue, err := d.readScheduledIssue(ctx, t)
	if err != nil {
		reservation.abandon()
		return false, err
	}
	if issue.State != github.StateOpen {
		reservation.abandon()
		if _, err = d.db.MoveBugFilingStartAttempt(ctx, a.ID, store.BugStartPending, store.BugStartReserved, "", 0, "", tickAt); err != nil {
			return false, err
		}
		a.State = store.BugStartReserved
		return false, d.closeBugStartFailed(ctx, f.ID, a, 0, "the issue is closed", tickAt)
	}
	ok, err := d.db.MoveBugFilingStartAttempt(ctx, a.ID, store.BugStartPending, store.BugStartReserved, "", 0, "", tickAt)
	if err != nil || !ok {
		reservation.abandon()
		return false, err
	}
	a.State = store.BugStartReserved
	t.capacity = reservation
	t.issue = issue
	d.inflight.label(session.ID, "bug_filing_start")
	go d.runDoctorGuardedWorker(sessionContext(context.WithoutCancel(ctx), session.ID), t, func(ctx context.Context) {
		d.runBugFilingStart(ctx, t, f, a)
	})
	return true, nil
}

// bugStartTrigger is the private, owner-authored trigger of one attempt.
func (d *Daemon) bugStartTrigger(ctx context.Context, a store.BugFilingStartAttempt) turn.Trigger {
	ids := d.ticketIdentities(ctx)
	return turn.Trigger{CommentID: a.TriggerID(), Body: bugStartBody, Author: ids.OwnerHandle, AuthorID: ids.OwnerUserID}
}

func (d *Daemon) runBugFilingStart(ctx context.Context, t *task, f store.BugFiling, a store.BugFilingStartAttempt) {
	now := d.brainstormNow()
	if ok, err := d.db.MoveBugFilingStartAttempt(ctx, a.ID, store.BugStartReserved, store.BugStartRunning, "", 0, "", now); err != nil || !ok {
		if err != nil {
			t.recordWorkerError(err)
		}
		return
	}
	a.State = store.BugStartRunning
	trigger := d.bugStartTrigger(ctx, a)
	t.commentUnderWay = trigger.CommentID
	runErr := d.run(ctx, t, trigger, turn.ParseCommand(trigger.Body), t.log)
	t.commentUnderWay = 0
	if runErr != nil {
		t.recordWorkerError(runErr)
	}
	if err := d.classifyBugStartAttempt(ctx, f, a, &runErr, d.brainstormNow()); err != nil {
		t.recordWorkerError(err)
		t.log.ErrorContext(ctx, "could not classify the bug filing start", slog.Any("error", err))
	}
}

// classifyBugStartAttempt reads the attempt's run and settles it one way only:
// deferred, still running, admitted or failed. handoff is nil when no hand-off
// happened in this process (replay or restart).
func (d *Daemon) classifyBugStartAttempt(ctx context.Context, f store.BugFiling, a store.BugFilingStartAttempt, handoff *error, now time.Time) error {
	run, exists, err := d.db.GetRunByTrigger(ctx, a.SessionID, a.TriggerID())
	if err != nil {
		return err
	}
	if !exists {
		if handoff != nil && *handoff != nil && !errors.Is(*handoff, context.Canceled) {
			return d.closeBugStartFailed(ctx, f.ID, a, 0, (*handoff).Error(), now)
		}
		_, err = d.db.MoveBugFilingStartAttempt(ctx, a.ID, a.State, store.BugStartPending, store.BugStartOutcomeDeferred, 0, "", now)
		return err
	}
	if !run.Reportable() {
		if run.Status == store.RunPrelaunch {
			_, err = d.db.MoveBugFilingStartAttempt(ctx, a.ID, a.State, store.BugStartPending, store.BugStartOutcomeDeferred, run.ID, "", now)
			return err
		}
		// Still running: the run's own recovery owns it; classify later.
		return nil
	}
	_, admitted, err := d.db.WorkflowLaunchInstance(ctx, a.SessionID, a.TriggerID())
	if err != nil {
		return err
	}
	if admitted {
		outcome := store.BugStartOutcomeStarted
		if handoff == nil {
			outcome = store.BugStartOutcomeReplayed
		}
		if _, err = d.db.MoveBugFilingStartAttempt(ctx, a.ID, a.State, store.BugStartSucceeded, outcome, run.ID, "", now); err != nil {
			return err
		}
		current, err := d.db.ReadBugFiling(ctx, f.ID)
		if err != nil {
			return err
		}
		ev, err := d.db.BugStartEvidence(ctx, a.SessionID)
		if err != nil {
			return err
		}
		return d.settleBugStart(ctx, current, ev, now)
	}
	reason := "Bug did not start"
	if run.Error != "" {
		reason = run.Error
	}
	return d.closeBugStartFailed(ctx, f.ID, a, run.ID, reason, now)
}

// closeBugStartFailed closes a terminal failure before admission and counts
// it; at the limit the filing ends not started.
func (d *Daemon) closeBugStartFailed(ctx context.Context, filingID int64, a store.BugFilingStartAttempt, runID int64, reason string, now time.Time) error {
	moved, err := d.db.MoveBugFilingStartAttempt(ctx, a.ID, a.State, store.BugStartFailed, store.BugStartOutcomeFailed, runID, reason, now)
	if err != nil || !moved {
		return err
	}
	current, err := d.db.ReadBugFiling(ctx, filingID)
	if err != nil {
		return err
	}
	if current.State != store.BugFilingLabelled && current.State != store.BugFilingStartAdmitted {
		return nil
	}
	next := current
	next.FailedStartAttempts++
	next.Reason = bugStartNotStarted
	if next.FailedStartAttempts >= store.BugStartFailureLimit && current.State == store.BugFilingLabelled {
		next.State = store.BugFilingStartFailed
		next.Reason = bugStartLimitReason(current)
	}
	_, err = d.db.SaveBugFiling(ctx, next, current.State, now)
	return err
}

// settleBugStart judges an admitted filing from its evidence: dispatched on
// an analyst attempt, failed when the instance ends or parks an owner wait
// first, or when S1 or S2 outlast the start wait. S2r stays partial while the
// instance is open.
func (d *Daemon) settleBugStart(ctx context.Context, f store.BugFiling, ev store.BugStartEvidence, now time.Time) error {
	if (f.State != store.BugFilingLabelled && f.State != store.BugFilingStartAdmitted) || !ev.Admitted {
		return nil
	}
	next := f
	next.State = store.BugFilingStartAdmitted
	if next.AdmissionTrigger == 0 {
		next.AdmissionTrigger = ev.AdmissionTrigger
	}
	phase := bugStartPhaseOf(ev)
	ended := ev.Instance.Status != store.GraphRunning && ev.Instance.Status != store.GraphPending
	switch {
	case phase == bugStartS3:
		next.State = store.BugFilingDispatched
		next.Reason = ""
	case ended:
		next.State = store.BugFilingStartFailed
		next.Reason = fmt.Sprintf("filed and labelled as %s but not started; the Bug analysis ended before the analyst", f.IssueLink)
	case ev.HasWait && ev.Wait.State == "waiting":
		next.State = store.BugFilingStartFailed
		next.Reason = bugStartParked
	case phase == bugStartS2r:
		next.Reason = bugStartRecorded
	default:
		// The wait restarts with each run after admission, so an owner's
		// reply gets its own wait on the same instance.
		since := ev.Instance.StartedAt
		for _, r := range ev.Runs {
			if r.StartedAt.After(since) {
				since = r.StartedAt
			}
		}
		if now.Sub(since) >= bugStartWait {
			next.State = store.BugFilingStartFailed
			next.Reason = fmt.Sprintf("filed and labelled as %s but not started; the Bug analysis was not dispatched within %s", f.IssueLink, bugStartWait)
		} else {
			next.Reason = bugStartAdmitted
		}
	}
	if next.State == f.State && next.Reason == f.Reason && next.AdmissionTrigger == f.AdmissionTrigger {
		return nil
	}
	_, err := d.db.SaveBugFiling(ctx, next, f.State, now)
	return err
}

func (d *Daemon) failBugStart(ctx context.Context, f store.BugFiling, reason string, now time.Time) error {
	next := f
	next.State = store.BugFilingStartFailed
	next.Reason = reason
	_, err := d.db.SaveBugFiling(ctx, next, f.State, now)
	return err
}

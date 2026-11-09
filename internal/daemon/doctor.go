package daemon

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The Doctor is an ordinary graph workflow on the task it repairs. The daemon
// only routes to it and starts it for a chat client or an owner action; the
// stopped-task rule and the evidence bundle live in its turn admission.

// operationalCall reserves a bounded request before background discovery. It
// shares quota observations and token accounting but uses the reserved
// operational share so a depleted background scan cannot wedge the control
// lane.
func operationalCall[T any](ctx context.Context, d *Daemon, call func(context.Context) (T, error)) (T, error) {
	b, err := d.quotaBudget(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	b.mu.Lock()
	if b.tokens < 1 || (b.limit > 0 && b.now.Before(b.reset) && b.remaining-b.reserved <= 0) {
		b.mu.Unlock()
		var zero T
		return zero, ErrDiscoveryDeferred
	}
	b.tokens--
	b.reserved++
	r := &discoveryReservation{budget: b, bound: 1, reset: b.reset}
	b.mu.Unlock()
	defer r.finish()
	if d.connections != nil {
		return call(context.WithValue(ctx, quotaReservationKey{}, r))
	}
	return call(github.WithRequestObserver(ctx, r.observe))
}

// ownerStops cancels the workers of runs the owner asked to stop from the
// factory host (`litefactory doctor stop`). Stopping is how an owner makes a
// task ready for the Doctor.
func (d *Daemon) ownerStops(ctx context.Context) error {
	runs, err := d.db.OwnerStopsRunning(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		d.inflight.cancel(run.SessionID, true)
	}
	return nil
}

// doctorRefusal is an owner-facing reason the Doctor did not start. Nothing
// was changed.
type doctorRefusal struct{ reason string }

func (e *doctorRefusal) Error() string { return e.reason }

func doctorStartedSummary(task string) string {
	return "The Doctor started on " + task + ". It reports on the task's issue and asks there when it needs you."
}

// doctorStartTrigger is the factory-private trigger of a Doctor start asked
// for outside the issue. It is a pure function of the request key in the
// factory work-id band, so a replayed request names the same start.
func doctorStartTrigger(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("doctor-start\x00" + key))
	return int64(h.Sum64()&(1<<56-1)) | 1<<56
}

// doctorRoute reports whether an owner comment belongs to the Doctor: an
// explicit /doctor, or any plain reply while the task's Doctor waits for the
// owner.
func (d *Daemon) doctorRoute(ctx context.Context, t *task, trigger turn.Trigger, command turn.Command) (bool, error) {
	if command.Kind == turn.KindDoctor {
		return true, nil
	}
	if command.Kind != turn.KindFreeText || command.Name != "" || t.continuation != nil || t.scheduled != nil ||
		t.implementing || t.refining || t.validating || store.IsFactoryTrigger(trigger.CommentID) || store.IsGraphTrigger(trigger.CommentID) {
		return false, nil
	}
	instance, err := d.db.LatestWorkflowInstance(ctx, t.session.ID, "doctor")
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return instance.Status == store.GraphRunning, nil
}

// startDoctor starts the Doctor on a task for an authenticated chat client or
// owner action. It refuses, with an owner-facing reason, a task that is not
// stopped or a factory without capacity.
func (d *Daemon) startDoctor(ctx context.Context, taskRef, note, key string) error {
	session, err := d.db.GetSessionByTaskRef(ctx, taskRef)
	if errors.Is(err, store.ErrNotFound) {
		return &doctorRefusal{"The Doctor did not start: " + taskRef + " has no factory work to diagnose."}
	}
	if err != nil {
		return err
	}
	if err := d.db.CheckTaskStopped(ctx, session.ID); err != nil {
		if errors.Is(err, store.ErrTaskNotStopped) {
			return &doctorRefusal{"The Doctor did not start: " + taskRef + " still has work in flight. Wait until it stops, then ask again."}
		}
		return err
	}
	repo, err := d.db.GetRepo(ctx, session.RepoID)
	if err != nil {
		return err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return err
	}
	if !d.enterSession(ctx, session.ID) {
		return &doctorRefusal{"The Doctor did not start: " + taskRef + " is busy right now. Ask again in a moment."}
	}
	handedOff := false
	defer func() {
		if !handedOff {
			d.inflight.leave(session.ID)
		}
	}()
	reservation, reason := d.reserveInput()
	if reservation == nil {
		return &doctorRefusal{"The Doctor did not start: the factory is at capacity (" + reason + "). Ask again later."}
	}
	tickAt := d.brainstormNow()
	log := d.log.With(slog.String("task", ref.String()), slog.Int64("session_id", session.ID), slog.String("dispatch_source", "doctor_start"))
	t := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, ref: ref, log: log, tickAt: tickAt}
	t.workerLog = log.With(slog.Any("dispatch_sources", []string{"doctor_start"}), slog.Time("tick_at", tickAt))
	issue, err := d.readScheduledIssue(ctx, t)
	if err != nil {
		reservation.abandon()
		return err
	}
	if issue.State != github.StateOpen {
		reservation.abandon()
		return &doctorRefusal{"The Doctor did not start: the issue of " + taskRef + " is closed."}
	}
	t.capacity = reservation
	t.issue = issue
	ids := d.ticketIdentities(ctx)
	body := turn.DoctorCommand
	if note != "" {
		body += " " + note
	}
	trigger := turn.Trigger{CommentID: doctorStartTrigger(key), Body: body, Author: ids.OwnerHandle, AuthorID: ids.OwnerUserID}
	d.inflight.label(session.ID, "doctor_start")
	handedOff = true
	go d.runGuardedWorker(sessionContext(context.WithoutCancel(ctx), session.ID), t, func(ctx context.Context) {
		t.commentUnderWay = trigger.CommentID
		if err := d.run(ctx, t, trigger, turn.ParseCommand(trigger.Body), t.log); err != nil {
			t.recordWorkerError(err)
		}
		t.commentUnderWay = 0
	})
	return nil
}

// doctorStarted reports whether the start a request key names was admitted.
func (d *Daemon) doctorStarted(ctx context.Context, taskRef, key string) (bool, error) {
	session, err := d.db.GetSessionByTaskRef(ctx, taskRef)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return d.db.WorkflowLaunched(ctx, session.ID, doctorStartTrigger(key))
}

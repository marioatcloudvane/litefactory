package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// enterDoctorGuarded holds admission across the second durable-hold check.
func (d *Daemon) enterDoctorGuarded(ctx context.Context, session int64) bool {
	held, err := d.doctorHeldSession(ctx, session)
	if err != nil || held {
		return false
	}
	if !d.inflight.enter(session) {
		return false
	}
	held, err = d.doctorHeldSession(ctx, session)
	if err != nil || held {
		d.inflight.leave(session)
		return false
	}
	return true
}
func (d *Daemon) runDoctorGuardedWorker(ctx context.Context, t *task, body func(context.Context)) {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.inflight.bindCancel(t.session.ID, cancel)
	d.runWorker(workerCtx, t, func() {
		held, err := d.doctorHeldSession(workerCtx, t.session.ID)
		if err != nil || held {
			cancel()
			return
		}
		body(workerCtx)
	})
}

// DrainDoctorTarget is a local factory control, never an agent tool. Admission
// is already fenced by a durable hold before cancellation. Parent writers are
// joined without cancelling unrelated parent work. Uncertain drain refuses edits.
func (d *Daemon) DrainDoctorTarget(ctx context.Context, ref naming.Ref, repairID string) (string, error) {
	operation, err := d.db.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		return "", err
	}
	held, err := d.db.DoctorRepairHeld(ctx, ref.String())
	if err != nil {
		return "", err
	}
	if !held || operation.ID != repairID || operation.Fenced {
		return "", errors.New("doctor target lacks active repair hold")
	}
	if err := d.db.CheckDoctorTargetStopped(ctx, ref.String()); err != nil {
		return "", err
	}
	target, err := d.db.GetSessionByTaskRef(ctx, ref.String())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	writers, err := d.db.DoctorRepairWriterSessions(ctx, ref.String())
	if err != nil {
		return "", err
	}
	for _, session := range writers {
		if err = d.inflight.drain(ctx, session, session == target.ID); err != nil {
			return "", err
		}
	}
	if d.instanceID == "" {
		return "", errors.New("doctor daemon instance identity unavailable")
	}
	return d.instanceID, nil
}

func (d *Daemon) doctorHeldSession(ctx context.Context, id int64) (bool, error) {
	// Lifecycle-only test objects have no store or production work; real daemons require one in New.
	if d.db == nil {
		return false, nil
	}
	return d.db.DoctorRepairHeldSession(ctx, id)
}

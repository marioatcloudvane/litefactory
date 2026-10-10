package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// StopDoctorRun cancels only the approved invocation through the owner-stop
// intent and joins the factory-owned worker before terminal settlement.
func (d *Daemon) StopDoctorRun(ctx context.Context, ref naming.Ref, id string, number int) error {
	r, err := d.db.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		return err
	}
	if r.ID != id {
		return errors.New("stop repair identity mismatch")
	}
	run, err := d.db.BeginDoctorStopRun(ctx, id, number)
	if err != nil {
		return err
	}
	if run.Status != "running" {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = d.inflight.drain(bounded, run.SessionID, true); err != nil {
		return err
	}
	return d.db.CompleteDoctorStopRun(ctx, id, number)
}

// StopRun uses authenticated local control to stop and join the approved invocation.
func (e *DoctorRepairEnvironment) StopRun(ctx context.Context, ref naming.Ref, id string, number int) error {
	live, ok := e.Live.(interface {
		StopDoctorRun(context.Context, naming.Ref, string, int) error
	})
	if !ok {
		return errors.New("authenticated live stop control unavailable")
	}
	return live.StopDoctorRun(ctx, ref, id, number)
}

package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/analystschedule"
)

// AnalystScheduleAdapter exposes the same one-pass orchestration used by the
// daemon. Callers supply the clock and capacity; Dispatch performs one pass.
type AnalystScheduleAdapter struct{ Service *analystschedule.Service }

// Dispatch performs at most one bounded pass using the supplied clock.
func (a AnalystScheduleAdapter) Dispatch(ctx context.Context, now time.Time) (bool, error) {
	if a.Service == nil {
		return false, nil
	}
	o, ok, e := a.Service.Reserve(ctx, now)
	if e != nil || !ok {
		return false, e
	}
	return true, a.Service.Run(ctx, o, now)
}

func (d *Daemon) dispatchAnalystSchedules(ctx context.Context, now time.Time, result *TickResult) error {
	if d.analystSchedules == nil {
		return nil
	}
	if _, held := d.connectionHold(ctx, now); held {
		return nil
	}
	if !d.pool.try() {
		return nil
	}
	// Reservation and reconciliation are bounded local bookkeeping. Provider work
	// starts only in the worker and shares the normal shutdown context and join.
	o, ok, e := d.analystSchedules.ReserveEligible(ctx, now, func(repoID int64) bool { _, held := d.connectionHold(projectContext(ctx, repoID), now); return !held })
	if e != nil || !ok {
		d.pool.done()
		return e
	}
	go func() {
		defer d.pool.done()
		if e := d.analystSchedules.Run(ctx, o, now); e != nil {
			d.log.ErrorContext(ctx, "scheduled analyst remains inspectable", slog.Int64("occurrence", o.ID), slog.Any("error", e))
		}
	}()
	result.Dispatched++
	return nil
}

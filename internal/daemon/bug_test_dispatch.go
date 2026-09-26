package daemon

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/store"
)

// runBugTestDispatch consumes the existing worker/dispatch queue without
// entering mode selection or launching an agent. The child's immutable call
// chooses the graph; labels and session-latest graph lookup cannot replace it.
func (d *Daemon) runBugTestDispatch(ctx context.Context, t *task, dispatch store.GraphDispatch) bool {
	call, err := d.db.BugTestDispatchCall(ctx, dispatch.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		t.recordWorkerError(err)
		return true
	}
	if d.bugTests == nil {
		t.log.ErrorContext(ctx, "Bug child check executor unavailable; dispatch remains pending", slog.Int64("call_id", call.ID))
		_, err = d.db.RequeueGraphDispatch(ctx, dispatch.ID, t.tickAt.UTC().Add(store.GraphDispatchRetryDelay(dispatch.Attempts)), "Bug child check executor unavailable")
		if err != nil {
			t.recordWorkerError(err)
		}
		return true
	}
	locked, err := d.db.AcquireSessionLock(ctx, dispatch.SessionID, d.instanceID)
	if err != nil {
		t.recordWorkerError(err)
		return true
	}
	if !locked {
		_, err = d.db.RequeueGraphDispatch(ctx, dispatch.ID, t.tickAt.UTC().Add(store.GraphDispatchRetryDelay(dispatch.Attempts)), "Bug child waits for task ownership")
		if err != nil {
			t.recordWorkerError(err)
		}
		return true
	}
	defer func() {
		if e := d.db.ReleaseSessionLock(context.WithoutCancel(ctx), dispatch.SessionID, d.instanceID); e != nil {
			t.recordWorkerError(e)
		}
	}()
	if err = d.bugTests.Advance(ctx, dispatch, t.tickAt.UTC()); err != nil {
		t.recordWorkerError(err)
		if recordErr := d.db.FailBugTestCall(context.WithoutCancel(ctx), call.ID, err.Error(), t.tickAt.UTC()); recordErr != nil {
			t.recordWorkerError(recordErr)
		}
		t.log.ErrorContext(ctx, "Bug child execution stopped with retained evidence", slog.Int64("call_id", call.ID), slog.Any("error", err))
	}
	return true
}

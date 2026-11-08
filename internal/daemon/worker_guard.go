package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// runGuardedWorker runs one task worker under the project's current approver
// authority: the worker claims that authority before it starts and releases it
// when it ends.
func (d *Daemon) runGuardedWorker(ctx context.Context, t *task, body func(context.Context)) {
	d.runProjectGuardedWorker(ctx, t, body, false)
}

func (d *Daemon) runTicketPollingWorker(ctx context.Context, t *task, body func(context.Context)) {
	d.runProjectGuardedWorker(ctx, t, body, true)
}

func (d *Daemon) runProjectGuardedWorker(ctx context.Context, t *task, body func(context.Context), codePolling bool) {
	ctx = sessionContext(ctx, t.session.ID)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.inflight.bindCancel(t.session.ID, cancel)
	d.runWorker(workerCtx, t, func() {
		if d.connections != nil {
			appointment, e := d.db.CurrentApprover(workerCtx, t.session.RepoID)
			if !codePolling && (errors.Is(e, store.ErrApproverMissing) || errors.Is(e, store.ErrApproverUnauthorized)) {
				body(workerCtx)
				return
			}
			key := fmt.Sprintf("%s:%d:%s", d.instanceID, t.session.ID, t.tickAt.Format(time.RFC3339Nano))
			var claim store.AuthorityClaim
			if e == nil {
				_, e = d.projectTicketIdentities(workerCtx, t.session.ID)
			}
			if e == nil {
				claim, e = d.db.ClaimProjectAuthority(workerCtx, store.AuthorityClaim{RepoID: t.session.RepoID, AppointmentID: appointment.ID, Revision: appointment.Revision, Actor: appointment.Actor, Kind: "worker", Key: key})
			}
			if codePolling && (errors.Is(e, store.ErrApproverMissing) || errors.Is(e, store.ErrConnectionDisabled) || errors.Is(e, store.ErrApproverUnauthorized)) {
				claim, e = d.claimCodePollingWorker(workerCtx, t.session.ID, key)
			}
			if e != nil {
				t.recordWorkerError(e)
				return
			}
			defer func() {
				if e := d.db.CompleteProjectAuthority(context.WithoutCancel(workerCtx), claim.ID); e != nil {
					t.recordWorkerError(e)
				}
			}()
		}
		body(workerCtx)
	})
}

// enterSession admits one worker for a session; it refuses while another worker
// of this daemon holds the session.
func (d *Daemon) enterSession(_ context.Context, session int64) bool {
	return d.inflight.enter(session)
}

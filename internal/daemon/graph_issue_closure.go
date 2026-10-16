package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Owner waits need fresh issue observations even when no work is scheduled.
// Reuse normal candidate rotation, discovery budgets and session exclusion.
//
// A Linear wait is re-added only on its repository's listing ticks: Linear has
// no free conditional read, so re-reading every wait on every heartbeat spent
// the allowance (#918). Webhook observations, recovery and the listing still
// offer a Linear wait whenever something new arrived; those reach seen first.
func (d *Daemon) graphOwnerWaits(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	sessions, err := d.db.ListGraphOwnerWaitSessions(ctx)
	if err != nil {
		return err
	}
	linearRepo := map[int64]bool{}
	for _, session := range sessions {
		if existing := seen[session.ID]; existing != nil {
			existing.addSource("graph_owner_wait")
			continue
		}
		q := byRepo[session.RepoID]
		if q == nil {
			continue
		}
		if !q.listing && d.connections != nil {
			linear, known := linearRepo[session.RepoID]
			if !known {
				v, err := d.db.CurrentConnection(ctx, session.RepoID, store.TicketConnection)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
				}
				linear = err == nil && v.Provider == ticket.Linear
				linearRepo[session.RepoID] = linear
			}
			if linear {
				continue
			}
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return fmt.Errorf("owner wait task ref %q: %w", session.TaskRef, err)
		}
		c := q.candidate(session, ref)
		c.addSource("graph_owner_wait")
		seen[session.ID] = c
		q.candidates = append(q.candidates, c)
	}
	return nil
}

// offer already owns in-process exclusion and the doctor guard. The durable
// session lock also excludes workers in other processes during cancellation.
func (d *Daemon) cancelClosedIssueGraphs(ctx context.Context, sessionID int64, at time.Time) (cleared bool, err error) {
	acquired, err := d.db.AcquireSessionLock(ctx, sessionID, d.instanceID)
	if err != nil || !acquired {
		return false, err
	}
	defer func() {
		err = errors.Join(err, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), sessionID, d.instanceID))
	}()
	err = d.db.CancelClosedIssueGraphs(ctx, sessionID, at)
	return err == nil, err
}

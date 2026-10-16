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
		if skip, err := d.linearOffListing(ctx, q, session.RepoID, linearRepo); err != nil {
			return err
		} else if skip {
			continue
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

// linearOffListing reports whether a standing candidate source must wait for
// its repository's listing tick: the repository uses a Linear ticket connection
// and this tick does not list it. Linear has no free conditional read, so a
// source re-offered on every heartbeat spends the allowance (#918, #920).
// known caches the per-repository answer within one tick.
func (d *Daemon) linearOffListing(ctx context.Context, q *repoQueue, repoID int64, known map[int64]bool) (bool, error) {
	if q.listing || d.connections == nil {
		return false, nil
	}
	linear, ok := known[repoID]
	if !ok {
		v, err := d.db.CurrentConnection(ctx, repoID, store.TicketConnection)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return false, err
		}
		linear = err == nil && v.Provider == ticket.Linear
		known[repoID] = linear
	}
	return linear, nil
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

package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// Owner waits need fresh issue observations even when no work is scheduled.
// Reuse normal candidate rotation, discovery budgets and session exclusion.
func (d *Daemon) graphOwnerWaits(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	sessions, err := d.db.ListGraphOwnerWaitSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if existing := seen[session.ID]; existing != nil {
			existing.addSource("graph_owner_wait")
			continue
		}
		q := byRepo[session.RepoID]
		if q == nil {
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

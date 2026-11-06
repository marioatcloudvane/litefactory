package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// BugStartEvidence is the retained state a bug filing's start step judges:
// the session's standalone Bug instance and the rows the Bug runtime itself
// keeps for its bootstrap and recovery. Every field is read-only evidence.
type BugStartEvidence struct {
	// Admitted reports that the session has a standalone Bug instance.
	Admitted bool
	Instance GraphInstance
	// AdmissionTrigger is the trigger of the instance's first retained launch.
	AdmissionTrigger int64
	Runs             []Run
	Attempts         []GraphNodeAttempt
	Transitions      []GraphTransition
	Dispatch         GraphDispatch
	HasDispatch      bool
	Wait             GraphOwnerWait
	HasWait          bool
	// HasComment reports any graph comment intent of the instance.
	HasComment bool
}

// BugStartEvidence reads the start evidence of one session. Without a Bug
// instance only the session's runs are read.
func (db *DB) BugStartEvidence(ctx context.Context, sessionID int64) (BugStartEvidence, error) {
	var ev BugStartEvidence
	var err error
	if ev.Runs, err = db.ListRuns(ctx, sessionID); err != nil {
		return ev, err
	}
	ev.Instance, err = db.LatestStandaloneBugInstance(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return ev, nil
	}
	if err != nil {
		return ev, fmt.Errorf("store: bug start evidence: %w", err)
	}
	ev.Admitted = true
	id := ev.Instance.ID
	err = db.sql.QueryRowContext(ctx, `SELECT trigger_id FROM research_launch_definitions WHERE instance_id=? ORDER BY id LIMIT 1`, id).Scan(&ev.AdmissionTrigger)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ev, fmt.Errorf("store: bug start admission: %w", err)
	}
	if ev.Attempts, err = db.ListGraphNodeAttempts(ctx, id); err != nil {
		return ev, err
	}
	if ev.Transitions, err = db.ListGraphTransitions(ctx, id); err != nil {
		return ev, err
	}
	ev.Dispatch, err = db.LatestGraphDispatch(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ev, err
	}
	ev.HasDispatch = err == nil
	ev.Wait, err = db.GetGraphOwnerWait(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ev, err
	}
	ev.HasWait = err == nil
	comments, err := db.ListGraphComments(ctx, id, "", 1)
	if err != nil {
		return ev, err
	}
	ev.HasComment = len(comments) > 0
	return ev, nil
}

package store

import (
	"context"
	"errors"
	"fmt"
)

// ErrRepairWorktreeHeld means saved repair work has not reached a verified
// reset or application boundary. Preparing another worker could destroy it.
var ErrRepairWorktreeHeld = errors.New("store: worktree is held by an unresolved Validate repair")

// CheckRepairWorktree refuses another worker while any graph in this session
// has an unfinished repair operation. The trusted caller can exempt only the
// exact repairing Validate instance so it can reconcile its own saved intent.
// Run this under existing session exclusion before any worktree preparation.
func (db *DB) CheckRepairWorktree(ctx context.Context, sessionID, allowedInstanceID int64) error {
	if sessionID < 1 || allowedInstanceID < 0 {
		return errors.New("store: invalid repair worktree guard identity")
	}
	var held bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM graph_instances g
 WHERE g.session_id=? AND g.id<>? AND (
  EXISTS(SELECT 1 FROM validate_repair_rounds r WHERE r.instance_id=g.id AND r.state='running')
  OR EXISTS(
   SELECT 1 FROM validate_repair_candidate_journal j WHERE j.instance_id=g.id AND (
    (j.phase='capture' AND NOT EXISTS(
      SELECT 1 FROM validate_repair_candidate_journal done
      WHERE done.candidate_id=j.candidate_id AND done.phase='reset' AND done.state='complete'))
    OR (j.phase IN ('reset','apply') AND j.state IN ('intent','blocked') AND NOT EXISTS(
      SELECT 1 FROM validate_repair_candidate_journal done
      WHERE done.candidate_id=j.candidate_id AND done.phase=j.phase AND done.state='complete'))
   )
  )
  OR EXISTS(
   SELECT 1 FROM validate_repair_candidates c
   JOIN validate_repair_candidate_decisions d ON d.candidate_id=c.id
   WHERE c.instance_id=g.id AND d.action='accept' AND NOT EXISTS(
    SELECT 1 FROM validate_repair_candidate_journal done
    WHERE done.candidate_id=c.id AND done.phase='apply' AND done.state='complete')
  )
 )
)`, sessionID, allowedInstanceID).Scan(&held)
	if err != nil {
		return fmt.Errorf("check retained repair worktree exclusion: %w", err)
	}
	if held {
		return ErrRepairWorktreeHeld
	}
	return nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// StoryProgressRun is the durable authority to consume one run transport directory.
type StoryProgressRun struct {
	RunID        int64
	Parent       string
	Generation   int
	Worktree     string
	Active       bool
	LastSequence int
	Status       RunStatus
}

// StoryProgressEvent retains accepted or rejected bytes independently of process exit.
type StoryProgressEvent struct {
	RunID        int64
	Sequence     int
	Story        string
	Attempt      int
	Kind, Digest string
	Payload      []byte
}

// StoryProgressEffect is an ordered, recoverable comment and optional state mutation.
type StoryProgressEffect struct {
	ID, RunID                                    int64
	Sequence                                     int
	Parent                                       string
	Generation                                   int
	Story                                        string
	IssueNumber                                  int
	IssueID, ParentID                            int64
	Revision, Kind, Detail, ImplementationDigest string
	CommentID                                    int64
}

// RegisterStoryProgress captures selected scope and launch admission atomically.
func (db *DB) RegisterStoryProgress(ctx context.Context, r StoryProgressRun) error {
	if r.RunID < 1 || r.Parent == "" || r.Generation < 1 || r.Worktree == "" {
		return fmt.Errorf("store: invalid story progress run")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO story_progress_runs(run_id,parent_task,generation,worktree) SELECT ?,?,?,? FROM runs r JOIN sessions s ON s.id=r.session_id JOIN managed_features f ON f.parent_task=s.task_ref WHERE r.id=? AND r.status IN ('prelaunch','running') AND r.mode='implement' AND s.task_ref=? AND f.generation=? AND f.state='implementing' ON CONFLICT(run_id) DO NOTHING`, r.RunID, r.Parent, r.Generation, r.Worktree, r.RunID, r.Parent, r.Generation)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var matching int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM story_progress_runs WHERE run_id=? AND parent_task=? AND generation=? AND worktree=? AND active=1`, r.RunID, r.Parent, r.Generation, r.Worktree).Scan(&matching); err != nil {
				return err
			}
			if matching != 1 {
				return fmt.Errorf("store: progress registration does not identify an active Implement run")
			}
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE managed_features SET engineering_succeeded=0 WHERE parent_task=? AND generation=?`, r.Parent, r.Generation); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE story_progress_runs SET aggregate_succeeded=0 WHERE parent_task=? AND generation=?`, r.Parent, r.Generation); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO story_progress_scope SELECT ?,s.story_key FROM managed_stories s JOIN approved_story_revisions a USING(parent_task,generation,story_key) WHERE s.parent_task=? AND s.generation=? AND s.state!='completed' AND s.engineering_complete=0`, r.RunID, r.Parent, r.Generation)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO story_progress_effects(run_id,sequence,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,kind,detail) SELECT ?,0,s.parent_task,s.generation,s.story_key,s.issue_number,s.issue_id,a.parent_id,a.digest,'started','Admitted for launch; this does not prove the process started. Parent checks, save and push remain pending.' FROM managed_stories s JOIN approved_story_revisions a USING(parent_task,generation,story_key) JOIN story_progress_scope p ON p.story_key=s.story_key AND p.run_id=? WHERE s.parent_task=? AND s.generation=?`, r.RunID, r.RunID, r.Parent, r.Generation)
		return err
	})
}

// ListStoryProgressRuns includes orphaned transports for restart recovery.
func (db *DB) ListStoryProgressRuns(ctx context.Context) ([]StoryProgressRun, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT p.run_id,p.parent_task,p.generation,p.worktree,p.active,p.last_sequence,r.status FROM story_progress_runs p JOIN runs r ON r.id=p.run_id WHERE p.active=1 ORDER BY p.run_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoryProgressRun
	for rows.Next() {
		var r StoryProgressRun
		if err := rows.Scan(&r.RunID, &r.Parent, &r.Generation, &r.Worktree, &r.Active, &r.LastSequence, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StoryProgressAuthorized excludes completed dependencies from outcome authority.
func (db *DB) StoryProgressAuthorized(ctx context.Context, runID int64, key string) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM story_progress_scope WHERE run_id=? AND story_key=?`, runID, key).Scan(&n)
	return n == 1, err
}

// GetStoryProgressEvent supplies original bytes for idempotent replay checks.
func (db *DB) GetStoryProgressEvent(ctx context.Context, runID int64, sequence int) (StoryProgressEvent, error) {
	var e StoryProgressEvent
	err := db.sql.QueryRowContext(ctx, `SELECT run_id,sequence,story_key,attempt,kind,digest,payload FROM story_progress_events WHERE run_id=? AND sequence=?`, runID, sequence).Scan(&e.RunID, &e.Sequence, &e.Story, &e.Attempt, &e.Kind, &e.Digest, &e.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// AcceptStoryProgress journals immutable evidence and the desired effect before
// any network I/O. Invalidation cancels all older effects on this story, across
// runs, so reconciliation cannot replay a stale close after reopening it.
func (db *DB) AcceptStoryProgress(ctx context.Context, e StoryProgressEvent, f StoryProgressEffect) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return acceptStoryProgressTx(ctx, tx, e, f) })
}
func acceptStoryProgressTx(ctx context.Context, tx *sql.Tx, e StoryProgressEvent, f StoryProgressEffect) error {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT digest FROM story_progress_events WHERE run_id=? AND sequence=?`, e.RunID, e.Sequence).Scan(&existing)
	if err == nil {
		if existing == e.Digest {
			return nil
		}
		return fmt.Errorf("store: conflicting story event replay")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var active bool
	var last int
	var parent string
	var generation int
	if err := tx.QueryRowContext(ctx, `SELECT active,last_sequence,parent_task,generation FROM story_progress_runs WHERE run_id=?`, e.RunID).Scan(&active, &last, &parent, &generation); err != nil {
		return err
	}
	if !active || last+1 != e.Sequence || parent != f.Parent || generation != f.Generation {
		return fmt.Errorf("store: story event is not next in active run")
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0) FROM story_progress_events WHERE run_id=? AND story_key=?`, e.RunID, e.Story).Scan(&attempt); err != nil {
		return err
	}
	if e.Attempt < attempt || e.Attempt > attempt+1 {
		return fmt.Errorf("store: invalid story attempt sequence")
	}
	var hash, state string
	var version int
	var engineered bool
	if err := tx.QueryRowContext(ctx, `SELECT a.digest,s.state,m.workflow_version,s.engineering_complete FROM approved_story_revisions a JOIN managed_stories s USING(parent_task,generation,story_key) JOIN story_progress_scope p ON p.story_key=s.story_key AND p.run_id=? JOIN managed_features m ON m.parent_task=s.parent_task AND m.generation=s.generation WHERE s.parent_task=? AND s.generation=? AND s.story_key=? AND s.issue_id=? AND s.issue_number=? AND a.parent_id=? AND m.state='implementing'`, e.RunID, f.Parent, f.Generation, e.Story, f.IssueID, f.IssueNumber, f.ParentID).Scan(&hash, &state, &version, &engineered); err != nil {
		return err
	}
	if (version == WorkflowSeparated && (e.Kind == "verified" || state == "completed")) || (version != WorkflowSeparated && e.Kind == "engineering_complete") || (engineered && e.Kind != "invalidated") {
		return fmt.Errorf("store: event conflicts with retained workflow or engineering state")
	}
	if hash != f.Revision || (e.Kind == "verified" && state == "completed") || (e.Kind == "blocked" && (state == "verified" || state == "completed")) {
		return fmt.Errorf("store: story revision or completed scope mismatch")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO story_progress_events(run_id,sequence,story_key,attempt,kind,digest,payload) VALUES(?,?,?,?,?,?,?)`, e.RunID, e.Sequence, e.Story, e.Attempt, e.Kind, e.Digest, e.Payload); err != nil {
		return err
	}
	if e.Kind == "engineering_complete" {
		if _, err := tx.ExecContext(ctx, `UPDATE managed_stories SET engineering_complete=1,state='frozen' WHERE parent_task=? AND generation=? AND story_key=?`, f.Parent, f.Generation, e.Story); err != nil {
			return err
		}
	} else {
		nextState := "blocked"
		if e.Kind == "verified" {
			nextState = "verified"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE managed_stories SET state=?,engineering_complete=0 WHERE parent_task=? AND generation=? AND story_key=?`, nextState, f.Parent, f.Generation, e.Story); err != nil {
			return err
		}
	}
	if e.Kind == "invalidated" {
		if _, err := tx.ExecContext(ctx, `UPDATE managed_features SET engineering_succeeded=0 WHERE parent_task=?`, f.Parent); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE story_progress_effects SET superseded=1 WHERE parent_task=? AND generation=? AND story_key=? AND delivered=0 AND kind!='started'`, f.Parent, f.Generation, e.Story); err != nil {
			return err
		}
	}
	if version == WorkflowSeparated && e.Kind == "invalidated" {
		f.Kind = "blocked"
	}
	if err := insertStoryEffect(ctx, tx, f); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE story_progress_runs SET last_sequence=? WHERE run_id=?`, e.Sequence, e.RunID)
	return err
}
func insertStoryEffect(ctx context.Context, tx *sql.Tx, f StoryProgressEffect) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO story_progress_effects(run_id,sequence,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,kind,detail,implementation_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, f.RunID, f.Sequence, f.Parent, f.Generation, f.Story, f.IssueNumber, f.IssueID, f.ParentID, f.Revision, f.Kind, f.Detail, f.ImplementationDigest)
	return err
}

// ListStoryProgressEffects returns only unsuperseded pending mutations, in order.
func (db *DB) ListStoryProgressEffects(ctx context.Context) ([]StoryProgressEffect, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,run_id,sequence,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,kind,detail,implementation_digest,comment_id FROM story_progress_effects WHERE delivered=0 AND superseded=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoryProgressEffect
	for rows.Next() {
		var f StoryProgressEffect
		if err := rows.Scan(&f.ID, &f.RunID, &f.Sequence, &f.Parent, &f.Generation, &f.Story, &f.IssueNumber, &f.IssueID, &f.ParentID, &f.Revision, &f.Kind, &f.Detail, &f.ImplementationDigest, &f.CommentID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveStoryProgressComment acknowledges the post before any close can be acknowledged.
func (db *DB) SaveStoryProgressComment(ctx context.Context, id, commentID int64) error {
	if commentID < 1 {
		return fmt.Errorf("store: missing progress comment identity")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE story_progress_effects SET comment_id=? WHERE id=? AND delivered=0 AND superseded=0`, commentID, id)
	return err
}

// DeliverStoryProgressEffect marks completion only after the remote effects succeed.
func (db *DB) DeliverStoryProgressEffect(ctx context.Context, f StoryProgressEffect) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE story_progress_effects SET delivered=1 WHERE id=? AND comment_id>0 AND superseded=0`, f.ID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: story progress effect superseded or not reported")
		}
		if f.Kind == "verified" {
			_, err = tx.ExecContext(ctx, `UPDATE managed_stories SET state='completed' WHERE parent_task=? AND generation=? AND story_key=? AND state='verified'`, f.Parent, f.Generation, f.Story)
		}
		return err
	})
}

// EndStoryProgress retires input authority and explains unfinished selected stories.
func (db *DB) EndStoryProgress(ctx context.Context, runID int64, reason string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if reason != "" {
			_, err := tx.ExecContext(ctx, `INSERT INTO story_progress_effects(run_id,sequence,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,kind,detail) SELECT p.run_id,p.last_sequence,s.parent_task,s.generation,s.story_key,s.issue_number,s.issue_id,a.parent_id,a.digest,'blocked',? FROM story_progress_runs p JOIN managed_stories s ON s.parent_task=p.parent_task AND s.generation=p.generation JOIN approved_story_revisions a USING(parent_task,generation,story_key) JOIN story_progress_scope c ON c.run_id=p.run_id AND c.story_key=s.story_key WHERE p.run_id=? AND p.active=1 AND s.state NOT IN ('completed','verified') AND s.engineering_complete=0`, reason, runID)
			if err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE story_progress_runs SET active=0 WHERE run_id=?`, runID)
		return err
	})
}

// ConflictStoryProgress revokes an accepted event exactly once if its immutable
// transport was replaced. The original bytes remain in the journal for review.
func (db *DB) ConflictStoryProgress(ctx context.Context, runID int64, sequence int) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE story_progress_events SET conflicted=1 WHERE run_id=? AND sequence=? AND conflicted=0`, runID, sequence)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		var parent, key string
		var generation int
		if err := tx.QueryRowContext(ctx, `SELECT p.parent_task,p.generation,e.story_key FROM story_progress_runs p JOIN story_progress_events e USING(run_id) WHERE e.run_id=? AND e.sequence=?`, runID, sequence).Scan(&parent, &generation, &key); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRowContext(ctx, `SELECT workflow_version FROM managed_features WHERE parent_task=?`, parent).Scan(&version); err != nil {
			return err
		}
		if version == WorkflowSeparated {
			// Historical acceptance cannot be revoked by later transport changes.
			// Before acceptance, withdraw both engineering facts and their pending
			// projection together, then record a corrective comment for recovery.
			res, err := tx.ExecContext(ctx, `UPDATE managed_stories SET engineering_complete=0,state='blocked' WHERE parent_task=? AND generation=? AND story_key=? AND state!='completed'`, parent, generation, key)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil || n == 0 {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE managed_features SET engineering_succeeded=0 WHERE parent_task=?`, parent); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE story_progress_effects SET superseded=1 WHERE parent_task=? AND generation=? AND story_key=? AND delivered=0 AND kind!='started'`, parent, generation, key); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO story_progress_effects(run_id,sequence,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,kind,detail) SELECT ?,?,s.parent_task,s.generation,s.story_key,s.issue_number,s.issue_id,a.parent_id,a.digest,'blocked','Immutable event was replaced with conflicting bytes; engineering evidence is withdrawn and fresh evidence is required. No acceptance or closure is claimed.' FROM managed_stories s JOIN approved_story_revisions a USING(parent_task,generation,story_key) WHERE s.parent_task=? AND s.generation=? AND s.story_key=?`, runID, sequence, parent, generation, key)
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE story_progress_effects SET superseded=1 WHERE parent_task=? AND generation=? AND story_key=? AND delivered=0 AND kind!='started'`, parent, generation, key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE managed_stories SET state='blocked' WHERE parent_task=? AND generation=? AND story_key=?`, parent, generation, key); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO story_progress_effects(run_id,sequence,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,kind,detail) SELECT ?,?,s.parent_task,s.generation,s.story_key,s.issue_number,s.issue_id,a.parent_id,a.digest,'invalidated','Immutable event was replaced with conflicting bytes; previous verification revoked.' FROM managed_stories s JOIN approved_story_revisions a USING(parent_task,generation,story_key) WHERE s.parent_task=? AND s.generation=? AND s.story_key=?`, runID, sequence, parent, generation, key)
		return err
	})
}

// RejectStoryProgress consumes a malformed envelope without giving its claimed
// identity any authority. Later valid invalidations must not be trapped behind
// an invalid checkpoint. Original rejected bytes remain auditable.
func (db *DB) RejectStoryProgress(ctx context.Context, runID int64, sequence int, digest string, payload []byte) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE story_progress_runs SET last_sequence=? WHERE run_id=? AND active=1 AND last_sequence=?`, sequence, runID, sequence-1)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: rejected event is not next")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO story_progress_events(run_id,sequence,story_key,attempt,kind,digest,payload) VALUES(?,?,'',0,'rejected',?,?)`, runID, sequence, digest, payload)
		return err
	})
}

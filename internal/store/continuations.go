package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MaxAutomaticAttempts bounds actual automatic run allocations per root interruption.
const MaxAutomaticAttempts = 3
const automaticTriggerBase int64 = 1 << 61

// ContinuationContext snapshots identity and the original instruction, never
// credentials or current permissions. Admission resolves permissions afresh.
type ContinuationContext struct {
	Workflow                                             string `json:",omitempty"`
	Task, Worktree, Agent, Mode, Request, ResumeIdentity string
	SessionRef                                           string
	SegmentOrdinal                                       int
	OwnerCommentID                                       int64
	ApprovedGeneration                                   int
}

// Continuation is durable automatic work. Attempt is consumed only when its
// run is allocated, not when this intent is recorded or reserved.
type Continuation struct {
	ID, SourceRunID, RootRunID, SessionID, SegmentID int64
	Attempt                                          int
	Origin                                           string
	DueAt, ResetBound                                time.Time
	Precision                                        string
	Context                                          ContinuationContext
	PrerequisiteID                                   int64
	Status                                           string
	AllocatedRunID                                   int64
	ReservedAt, FinishedAt                           time.Time
	CancellationReason                               string
	AnnouncementCommentID                            int64
	CreatedAt                                        time.Time
}

// TriggerID returns the private execution identity, distinct from forge comments
// and manual schedule triggers.
func (c Continuation) TriggerID() int64 { return automaticTriggerBase + c.ID }

// IsAutomaticTrigger reports whether an identity belongs to automatic recovery.
func IsAutomaticTrigger(id int64) bool { return id > automaticTriggerBase && id < scheduleTriggerBase }

// IsPrivateTrigger reports whether an identity was minted by the factory rather
// than issued by the forge. Every such identity is larger than any comment id a
// forge will ever produce, so treating one as a comment — above all, advancing
// the comment watermark to it — silently deafens the task to everything the
// owner says afterwards. Ask this rather than naming one band: a caller that
// checks only the band it had in mind is how that happened once already.
func IsPrivateTrigger(id int64) bool {
	return IsGraphTrigger(id) || IsAutomaticTrigger(id) || IsScheduleTrigger(id)
}

// FinishInterruptedRun commits evidence and its optional continuation together.
// Replayed completion returns the immutable original decision, even after report.
func (db *DB) FinishInterruptedRun(ctx context.Context, runID int64, outcome RunOutcome, intent *Continuation, resumeToken string) (run Run, continuation *Continuation, err error) {
	if outcome.SemanticOutcome != "interrupted" || outcome.Status != RunFailed {
		return Run{}, nil, errors.New("store: interruption requires semantic failed outcome")
	}
	finished := outcome.FinishedAt
	if finished.IsZero() {
		return Run{}, nil, errors.New("store: interruption observation time is required")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		current, e := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, runID))
		if e != nil {
			return e
		}
		if current.SemanticOutcome == "interrupted" {
			run = current
			c, e := getContinuationBySource(ctx, tx, runID)
			if errors.Is(e, ErrNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			continuation = &c
			return nil
		}
		if current.Status != RunRunning {
			return errors.New("run already has a different terminal outcome")
		}
		if resumeToken != "" {
			if _, e := tx.ExecContext(ctx, `UPDATE segments SET resume_token = ? WHERE id = ?`, resumeToken, current.SegmentID); e != nil {
				return e
			}
		}
		run, e = finishRunTx(ctx, tx, runID, outcome, finished)
		if e != nil {
			return e
		}
		var graph bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.run_id=? AND d.mode IN ('research','spike'))`, runID).Scan(&graph); e != nil {
			return e
		}
		if intent == nil || graph {
			return settleContinuationRun(ctx, tx, run)
		}
		c := *intent
		c.SourceRunID, c.SessionID, c.SegmentID = runID, current.SessionID, current.SegmentID
		c.RootRunID, c.Attempt = runID, 1
		prior, e := scanContinuation(tx.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE allocated_run_id = ?`, runID))
		if e == nil {
			c.RootRunID, c.Attempt, c.PrerequisiteID = prior.RootRunID, prior.Attempt+1, prior.PrerequisiteID
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		if c.Attempt > MaxAutomaticAttempts {
			return errors.New("automatic continuation budget exhausted")
		}
		if c.Context.ResumeIdentity == "" || c.Context.Request == "" || c.Context.Task == "" || c.Context.Worktree == "" || !c.ResetBound.After(finished) || c.DueAt.Before(c.ResetBound.Add(time.Minute)) {
			return errors.New("automatic continuation lacks reliable context or future reset")
		}
		if c.Context.Agent != current.Agent || c.Context.Mode != current.Mode {
			return errors.New("continuation context disagrees with interrupted run")
		}
		if c.PrerequisiteID != 0 {
			source, e := scanSchedule(tx.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE id = ?`, c.PrerequisiteID))
			if e != nil {
				return e
			}
			if source.SessionID != current.SessionID {
				return errors.New("continuation prerequisite belongs to another task")
			}
		}
		data, e := json.Marshal(c.Context)
		if e != nil {
			return e
		}
		c, e = scanContinuation(tx.QueryRowContext(ctx, `INSERT INTO automatic_continuations
   (source_run_id,root_run_id,session_id,segment_id,attempt,due_at,reset_bound,precision,context_json,prerequisite_id,created_at)
   VALUES (?,?,?,?,?,?,?,?,?,?,?) RETURNING `+continuationColumns,
			runID, c.RootRunID, c.SessionID, c.SegmentID, c.Attempt, FormatTime(c.DueAt), FormatTime(c.ResetBound), c.Precision, string(data), nullID(c.PrerequisiteID), FormatTime(finished)))
		if e != nil {
			return e
		}
		if c.PrerequisiteID != 0 {
			if _, e := tx.ExecContext(ctx, `UPDATE schedules SET status = 'suspended', recovery_root_run_id = ? WHERE id = ? AND session_id = ?`, c.RootRunID, c.PrerequisiteID, c.SessionID); e != nil {
				return e
			}
		}
		continuation = &c
		return settleContinuationRun(ctx, tx, run)
	})
	if err != nil {
		return Run{}, nil, fmt.Errorf("store: finish interrupted run %d: %w", runID, err)
	}
	return run, continuation, nil
}

// GetContinuation loads a persisted action by its identity.
func (db *DB) GetContinuation(ctx context.Context, id int64) (Continuation, error) {
	return scanContinuation(db.sql.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE id = ?`, id))
}

// GetContinuationBySource loads the unique action created by an interrupted run.
func (db *DB) GetContinuationBySource(ctx context.Context, runID int64) (Continuation, error) {
	return getContinuationBySource(ctx, db.sql, runID)
}
func getContinuationBySource(ctx context.Context, q querier, runID int64) (Continuation, error) {
	return scanContinuation(q.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE source_run_id = ?`, runID))
}

// GetContinuationByRun loads the action which allocated an automatic run.
func (db *DB) GetContinuationByRun(ctx context.Context, runID int64) (Continuation, error) {
	return scanContinuation(db.sql.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE allocated_run_id = ?`, runID))
}

// ListPendingInterruptionReports is deliberately independent of owner watermarks.
func (db *DB) ListPendingInterruptionReports(ctx context.Context) ([]Run, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+runColumns+` FROM runs WHERE semantic_outcome = 'interrupted' AND result_comment_id IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

// ListPendingContinuations includes future work so admission can cancel stale
// intent before its deadline. A zero sessionID selects all sessions.
func (db *DB) ListPendingContinuations(ctx context.Context, sessionID int64) ([]Continuation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE status = 'pending' AND (? = 0 OR session_id = ?) ORDER BY due_at,id`, sessionID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Continuation
	for rows.Next() {
		c, err := scanContinuation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

const continuationColumns = `id,source_run_id,root_run_id,session_id,segment_id,attempt,origin,due_at,reset_bound,precision,context_json,prerequisite_id,status,allocated_run_id,reserved_at,finished_at,cancellation_reason,announcement_comment_id,created_at`

func scanContinuation(s scanner) (Continuation, error) {
	var c Continuation
	var due, reset, reserved, finished, created sql.NullString
	var prerequisite, allocated, announcement sql.NullInt64
	var snapshot string
	err := s.Scan(&c.ID, &c.SourceRunID, &c.RootRunID, &c.SessionID, &c.SegmentID, &c.Attempt, &c.Origin, &due, &reset, &c.Precision, &snapshot, &prerequisite, &c.Status, &allocated, &reserved, &finished, &c.CancellationReason, &announcement, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.PrerequisiteID, c.AllocatedRunID, c.AnnouncementCommentID = prerequisite.Int64, allocated.Int64, announcement.Int64
	for _, f := range []struct {
		src sql.NullString
		dst *time.Time
	}{{due, &c.DueAt}, {reset, &c.ResetBound}, {reserved, &c.ReservedAt}, {finished, &c.FinishedAt}, {created, &c.CreatedAt}} {
		t, e := scanTime(f.src)
		if e != nil {
			return c, e
		}
		*f.dst = t
	}
	if err := json.Unmarshal([]byte(snapshot), &c.Context); err != nil {
		return c, fmt.Errorf("decode continuation context: %w", err)
	}
	return c, nil
}

// GetScheduleByTrigger decodes only a factory-private manual schedule identity.
func (db *DB) GetScheduleByTrigger(ctx context.Context, triggerID int64) (Schedule, error) {
	if !IsScheduleTrigger(triggerID) {
		return Schedule{}, ErrNotFound
	}
	return db.GetSchedule(ctx, triggerID-scheduleTriggerBase)
}

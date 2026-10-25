package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Repair round states. A running round is unfinished; every other state is
// final and immutable.
const (
	RepairRunning     = "running"
	RepairKept        = "kept"
	RepairTooBig      = "too_big"
	RepairHandback    = "handback"
	RepairBlocked     = "blocked"
	RepairTimedOut    = "timed_out"
	RepairInterrupted = "interrupted"
	// RepairUnusable is a round whose agent finished but left a result or a
	// change the factory cannot use. It counts as used.
	RepairUnusable = "unusable"
)

// MaxRepairRounds is the most repair rounds one Validate instance can record.
// The graph declares the bound it uses; this is the hard ceiling under it.
const MaxRepairRounds = 2

// MaxRepairDiffBytes bounds the diff a round retains as evidence.
const MaxRepairDiffBytes = 64 << 10

var (
	// ErrRepairRoundExists refuses a second row for the same instance and round.
	ErrRepairRoundExists = errors.New("store: repair round already recorded")
	// ErrRepairRoundLimit refuses a round beyond the ceiling.
	ErrRepairRoundLimit = errors.New("store: repair round limit reached")
	// ErrRepairFinished refuses a change to a finished repair round.
	ErrRepairFinished = errors.New("store: finished repair round cannot change")
)

// RepairRound is one repair attempt of one Validate graph instance.
type RepairRound struct {
	ID            int64
	InstanceID    int64
	Round         int
	SuiteRunID    int64
	PreCommit     string
	NodeAttemptID int64
	Response      string
	State         string
	Reason        string
	Files, Kinds  []string
	Diagnosis     string
	DiffDigest    string
	Diff          string
	ResultCommit  string
	StartedAt     time.Time
	FinishedAt    time.Time
}

const repairColumns = `id,instance_id,round,suite_run_id,pre_commit,node_attempt_id,response,state,reason,files,kinds,diagnosis,diff_digest,diff,result_commit,started_at,finished_at`

// BeginRepairRound records a running round. The round number is the next one
// after the instance's latest. It is counted as used from this moment, so a
// round that never finishes still counts.
func (db *DB) BeginRepairRound(ctx context.Context, in RepairRound) (RepairRound, error) {
	if in.InstanceID < 1 || len(in.PreCommit) != 40 && len(in.PreCommit) != 64 || in.StartedAt.IsZero() {
		return RepairRound{}, errors.New("store: invalid repair round")
	}
	var out RepairRound
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var round int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(round),0)+1 FROM validate_repair_rounds WHERE instance_id=?`, in.InstanceID).Scan(&round); err != nil {
			return err
		}
		if round > MaxRepairRounds {
			return ErrRepairRoundLimit
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO validate_repair_rounds(instance_id,round,suite_run_id,pre_commit,node_attempt_id,state,started_at) VALUES(?,?,?,?,?,'running',?)`,
			in.InstanceID, round, in.SuiteRunID, in.PreCommit, in.NodeAttemptID, FormatTime(in.StartedAt))
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		out = in
		out.ID, out.Round, out.State = id, round, RepairRunning
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRepairRoundLimit) {
			return RepairRound{}, err
		}
		return RepairRound{}, fmt.Errorf("store: begin repair round: %w", err)
	}
	return out, nil
}

// RepairFinish is the final content of a repair round.
type RepairFinish struct {
	NodeAttemptID int64
	Response      string
	State         string
	Reason        string
	Files, Kinds  []string
	Diagnosis     string
	DiffDigest    string
	Diff          string
	ResultCommit  string
	At            time.Time
}

// FinishRepairRound makes a running round final. Finishing a finished round with
// the same content succeeds; different content is ErrRepairFinished.
func (db *DB) FinishRepairRound(ctx context.Context, id int64, in RepairFinish) error {
	if id < 1 || in.State == RepairRunning || !slices.Contains([]string{RepairKept, RepairTooBig, RepairHandback, RepairBlocked, RepairTimedOut, RepairInterrupted, RepairUnusable}, in.State) || in.At.IsZero() {
		return errors.New("store: invalid repair round finish")
	}
	if (in.State == RepairKept) != (in.ResultCommit != "") {
		return errors.New("store: only a kept repair round has a resulting commit")
	}
	if len(in.Diff) > MaxRepairDiffBytes {
		in.Diff = in.Diff[:MaxRepairDiffBytes]
	}
	files, err := json.Marshal(nonNil(in.Files))
	if err != nil {
		return err
	}
	kinds, err := json.Marshal(nonNil(in.Kinds))
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanRepair(tx.QueryRowContext(ctx, `SELECT `+repairColumns+` FROM validate_repair_rounds WHERE id=?`, id))
		if err != nil {
			return err
		}
		if cur.State != RepairRunning {
			if cur.State == in.State && cur.Reason == in.Reason && cur.ResultCommit == in.ResultCommit && cur.DiffDigest == in.DiffDigest {
				return nil
			}
			return ErrRepairFinished
		}
		if in.NodeAttemptID == 0 {
			in.NodeAttemptID = cur.NodeAttemptID
		}
		_, err = tx.ExecContext(ctx, `UPDATE validate_repair_rounds SET node_attempt_id=?,response=?,state=?,reason=?,files=?,kinds=?,diagnosis=?,diff_digest=?,diff=?,result_commit=?,finished_at=? WHERE id=? AND state='running'`,
			in.NodeAttemptID, in.Response, in.State, in.Reason, string(files), string(kinds), in.Diagnosis, in.DiffDigest, in.Diff, in.ResultCommit, FormatTime(in.At), id)
		return err
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ListRepairRounds returns every round of an instance in round order.
func (db *DB) ListRepairRounds(ctx context.Context, instanceID int64) ([]RepairRound, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+repairColumns+` FROM validate_repair_rounds WHERE instance_id=? ORDER BY round`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("store: list repair rounds: %w", err)
	}
	defer rows.Close()
	var out []RepairRound
	for rows.Next() {
		r, err := scanRepair(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRepairRound returns one round or ErrNotFound.
func (db *DB) GetRepairRound(ctx context.Context, id int64) (RepairRound, error) {
	return scanRepair(db.sql.QueryRowContext(ctx, `SELECT `+repairColumns+` FROM validate_repair_rounds WHERE id=?`, id))
}

// RunningRepairRound returns the unfinished round of an instance, or ErrNotFound.
func (db *DB) RunningRepairRound(ctx context.Context, instanceID int64) (RepairRound, error) {
	return scanRepair(db.sql.QueryRowContext(ctx, `SELECT `+repairColumns+` FROM validate_repair_rounds WHERE instance_id=? AND state='running'`, instanceID))
}

func scanRepair(row scanner) (RepairRound, error) {
	var r RepairRound
	var files, kinds, started, finished string
	if err := row.Scan(&r.ID, &r.InstanceID, &r.Round, &r.SuiteRunID, &r.PreCommit, &r.NodeAttemptID, &r.Response, &r.State, &r.Reason, &files, &kinds, &r.Diagnosis, &r.DiffDigest, &r.Diff, &r.ResultCommit, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RepairRound{}, ErrNotFound
		}
		return RepairRound{}, fmt.Errorf("store: read repair round: %w", err)
	}
	if err := json.Unmarshal([]byte(files), &r.Files); err != nil {
		return RepairRound{}, fmt.Errorf("store: read repair round: %w", err)
	}
	if err := json.Unmarshal([]byte(kinds), &r.Kinds); err != nil {
		return RepairRound{}, fmt.Errorf("store: read repair round: %w", err)
	}
	var err error
	if r.StartedAt, err = ParseTime(started); err != nil {
		return RepairRound{}, err
	}
	if finished != "" {
		if r.FinishedAt, err = ParseTime(finished); err != nil {
			return RepairRound{}, err
		}
	}
	return r, nil
}

// AttemptTimeout records that a graph agent attempt ran out of its time budget.
type AttemptTimeout struct {
	NodeAttemptID int64
	Budget        time.Duration
	TimedOutAt    time.Time
}

// RecordAttemptTimeout retains a timeout once. A second record for the same
// attempt is ignored: the first fact stands.
func (db *DB) RecordAttemptTimeout(ctx context.Context, in AttemptTimeout) error {
	if in.NodeAttemptID < 1 || in.Budget <= 0 || in.TimedOutAt.IsZero() {
		return errors.New("store: invalid attempt timeout")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error { return recordAttemptTimeoutTx(ctx, tx, in) })
}

func recordAttemptTimeoutTx(ctx context.Context, tx *sql.Tx, in AttemptTimeout) error {
	inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO graph_attempt_timeouts(node_attempt_id,budget_seconds,timed_out_at,budget_ns) VALUES(?,?,?,?)`, in.NodeAttemptID, int64((in.Budget-1)/time.Second)+1, FormatTime(in.TimedOutAt), int64(in.Budget))
	if err != nil {
		return fmt.Errorf("store: record attempt timeout: %w", err)
	}
	n, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	// Hold authority and timeout evidence become durable together. This runs
	// before best-effort evidence cleanup or any remote notice delivery.
	_, err = tx.ExecContext(ctx, `UPDATE implement_stories SET timeout_attempt_id=?,timeout_park_revision=revision+1,last_failure=?,updated_at=? WHERE state='running' AND timeout_attempt_id=0 AND worker_session IN (SELECT i.session_id FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id JOIN graph_node_attempts a ON a.instance_id=i.id WHERE a.id=? AND d.mode='implement')`, in.NodeAttemptID, fmt.Sprintf("Worker timed out after %s; available work is retained. Additional process and cleanup evidence is unavailable until collection completes.", in.Budget), FormatTime(in.TimedOutAt), in.NodeAttemptID)
	if err != nil {
		return fmt.Errorf("store: hold timed-out implement story: %w", err)
	}
	return nil
}

// GetAttemptTimeout returns the timeout of an attempt or ErrNotFound.
func (db *DB) GetAttemptTimeout(ctx context.Context, attemptID int64) (AttemptTimeout, error) {
	var seconds int64
	var ns sql.NullInt64
	var at string
	err := db.sql.QueryRowContext(ctx, `SELECT budget_seconds,timed_out_at,budget_ns FROM graph_attempt_timeouts WHERE node_attempt_id=?`, attemptID).Scan(&seconds, &at, &ns)
	if errors.Is(err, sql.ErrNoRows) {
		return AttemptTimeout{}, ErrNotFound
	}
	if err != nil {
		return AttemptTimeout{}, fmt.Errorf("store: read attempt timeout: %w", err)
	}
	t, err := ParseTime(at)
	if err != nil {
		return AttemptTimeout{}, err
	}
	budget := time.Duration(seconds) * time.Second
	if ns.Valid {
		budget = time.Duration(ns.Int64)
	}
	return AttemptTimeout{NodeAttemptID: attemptID, Budget: budget, TimedOutAt: t}, nil
}

// LatestKeptRepairCommit returns the commit of the newest kept repair round of an
// instance, including verified owner adoptions, or ErrNotFound when none exists.
// Pending and discarded candidates never select the commit the suite runs on.
func (db *DB) LatestKeptRepairCommit(ctx context.Context, instanceID int64) (string, error) {
	var commit string
	err := db.sql.QueryRowContext(ctx, `SELECT result_commit FROM (
 SELECT result_commit,round AS repair_round FROM validate_repair_rounds WHERE instance_id=? AND state='kept'
 UNION ALL
 SELECT j.result_commit,r.round AS repair_round FROM validate_repair_candidate_journal j
 JOIN validate_repair_candidate_decisions d ON d.candidate_id=j.candidate_id
 JOIN validate_repair_candidates c ON c.id=j.candidate_id
 JOIN validate_repair_rounds r ON r.id=c.round_id
 WHERE c.instance_id=? AND j.phase='apply' AND j.state='complete' AND d.action='accept'
 ) ORDER BY repair_round DESC LIMIT 1`, instanceID, instanceID).Scan(&commit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: read kept repair commit: %w", err)
	}
	return commit, nil
}

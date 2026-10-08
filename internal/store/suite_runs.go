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

// Suite run and command states. A running row is unfinished; every other state
// is final and immutable.
const (
	SuiteRunning     = "running"
	SuitePassed      = "passed"
	SuiteFailed      = "failed"
	SuiteBlocked     = "blocked"
	SuiteCancelled   = "cancelled"
	SuiteInterrupted = "interrupted"
)

// MaxSuiteOutputTail bounds the retained tail of one command's output.
const MaxSuiteOutputTail = 16 << 10

var (
	// ErrSuiteFinished refuses a change to a finished suite run or command.
	ErrSuiteFinished = errors.New("store: finished suite record cannot change")
	// ErrSuiteRunning refuses a second running suite run.
	ErrSuiteRunning = errors.New("store: a suite run is already running")
)

func validSuiteState(s string) bool {
	return slices.Contains([]string{SuiteRunning, SuitePassed, SuiteFailed, SuiteBlocked, SuiteCancelled, SuiteInterrupted}, s)
}

// SuiteRun is one attempt to run the full suite for one graph instance on one
// exact commit.
type SuiteRun struct {
	ID           int64
	InstanceID   int64
	Node         string
	Attempt      int
	Commit       string
	RecipeDigest string
	TreeBefore   string
	TreeAfter    string
	State        string
	Reason       string
	StartedAt    time.Time
	FinishedAt   time.Time
}

// SuiteCommand is one declared command of a suite run.
type SuiteCommand struct {
	RunID            int64
	Position         int
	Argv             []string
	State            string
	Exit             *int
	Reason           string
	StartedAt        time.Time
	FinishedAt       time.Time
	ProcessIdentity  string
	CleanupConfirmed bool
	OutputTail       string
	OutputTruncated  bool
	OutputDigest     string
}

const suiteRunColumns = `id,instance_id,node,attempt,commit_sha,recipe_digest,tree_before,tree_after,state,reason,started_at,finished_at`

// BeginSuiteRun records a new running attempt. The attempt number follows the
// latest attempt of the same instance, commit and recipe digest. A second
// running attempt anywhere in the home is ErrSuiteRunning.
func (db *DB) BeginSuiteRun(ctx context.Context, in SuiteRun) (SuiteRun, error) {
	if in.InstanceID < 1 || in.Node == "" || len(in.Commit) != 40 && len(in.Commit) != 64 || in.StartedAt.IsZero() {
		return SuiteRun{}, fmt.Errorf("store: invalid suite run")
	}
	var out SuiteRun
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var running int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM suite_runs WHERE state='running'`).Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return ErrSuiteRunning
		}
		var attempt int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0)+1 FROM suite_runs WHERE instance_id=? AND commit_sha=? AND recipe_digest=?`, in.InstanceID, in.Commit, in.RecipeDigest).Scan(&attempt); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO suite_runs(instance_id,node,attempt,commit_sha,recipe_digest,tree_before,state,started_at) VALUES(?,?,?,?,?,?,'running',?)`,
			in.InstanceID, in.Node, attempt, in.Commit, in.RecipeDigest, in.TreeBefore, FormatTime(in.StartedAt))
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		out = in
		out.ID, out.Attempt, out.State = id, attempt, SuiteRunning
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrSuiteRunning) {
			return SuiteRun{}, err
		}
		return SuiteRun{}, fmt.Errorf("store: begin suite run: %w", err)
	}
	return out, nil
}

// FinishSuiteRun makes a running attempt final. Finishing an already finished
// attempt with the same content succeeds; different content is ErrSuiteFinished.
func (db *DB) FinishSuiteRun(ctx context.Context, id int64, state, treeAfter, reason string, at time.Time) error {
	if id < 1 || !validSuiteState(state) || state == SuiteRunning || at.IsZero() {
		return fmt.Errorf("store: invalid suite run finish")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanSuiteRun(tx.QueryRowContext(ctx, `SELECT `+suiteRunColumns+` FROM suite_runs WHERE id=?`, id))
		if err != nil {
			return err
		}
		if cur.State != SuiteRunning {
			if cur.State == state && cur.TreeAfter == treeAfter && cur.Reason == reason {
				return nil
			}
			return ErrSuiteFinished
		}
		_, err = tx.ExecContext(ctx, `UPDATE suite_runs SET state=?,tree_after=?,reason=?,finished_at=? WHERE id=? AND state='running'`, state, treeAfter, reason, FormatTime(at), id)
		return err
	})
}

// InterruptSuiteRun marks a running attempt and its running commands
// interrupted. The caller has confirmed that no process of it survives.
func (db *DB) InterruptSuiteRun(ctx context.Context, id int64, at time.Time) error {
	if id < 1 || at.IsZero() {
		return fmt.Errorf("store: invalid suite run interruption")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		stamp := FormatTime(at)
		if _, err := tx.ExecContext(ctx, `UPDATE suite_commands SET state='interrupted',reason='interrupted by a restart',finished_at=? WHERE run_id=? AND state='running'`, stamp, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE suite_runs SET state='interrupted',reason='interrupted by a restart',finished_at=? WHERE id=? AND state='running'`, stamp, id)
		return err
	})
}

// LatestSuiteRun returns the newest attempt of an instance on a commit, or ErrNotFound.
func (db *DB) LatestSuiteRun(ctx context.Context, instanceID int64, commit string) (SuiteRun, error) {
	return scanSuiteRun(db.sql.QueryRowContext(ctx, `SELECT `+suiteRunColumns+` FROM suite_runs WHERE instance_id=? AND commit_sha=? ORDER BY id DESC LIMIT 1`, instanceID, commit))
}

// ReuseSuiteRun copies the newest passed attempt of another instance on the
// same commit, with its commands, into instanceID as that instance's next
// attempt. The commit fixes the recipe, so the result still holds. No passed
// attempt is ErrNotFound.
func (db *DB) ReuseSuiteRun(ctx context.Context, instanceID int64, node, commit string, at time.Time) (SuiteRun, error) {
	if instanceID < 1 || node == "" || len(commit) != 40 && len(commit) != 64 || at.IsZero() {
		return SuiteRun{}, fmt.Errorf("store: invalid suite run reuse")
	}
	var out SuiteRun
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		src, err := scanSuiteRun(tx.QueryRowContext(ctx, `SELECT `+suiteRunColumns+` FROM suite_runs WHERE commit_sha=? AND state='passed' AND instance_id<>? ORDER BY id DESC LIMIT 1`, commit, instanceID))
		if err != nil {
			return err
		}
		var attempt int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0)+1 FROM suite_runs WHERE instance_id=? AND commit_sha=? AND recipe_digest=?`, instanceID, commit, src.RecipeDigest).Scan(&attempt); err != nil {
			return err
		}
		reason := fmt.Sprintf("reused passed suite run %d of instance %d", src.ID, src.InstanceID)
		stamp := FormatTime(at)
		res, err := tx.ExecContext(ctx, `INSERT INTO suite_runs(instance_id,node,attempt,commit_sha,recipe_digest,tree_before,tree_after,state,reason,started_at,finished_at) VALUES(?,?,?,?,?,?,?,'passed',?,?,?)`,
			instanceID, node, attempt, commit, src.RecipeDigest, src.TreeBefore, src.TreeAfter, reason, stamp, stamp)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO suite_commands(run_id,position,argv,state,exit_code,reason,started_at,finished_at,process_identity,cleanup_confirmed,output_tail,output_truncated,output_digest)
SELECT ?,position,argv,state,exit_code,reason,started_at,finished_at,process_identity,cleanup_confirmed,output_tail,output_truncated,output_digest FROM suite_commands WHERE run_id=?`, id, src.ID); err != nil {
			return err
		}
		out = src
		out.ID, out.InstanceID, out.Node, out.Attempt, out.Reason, out.StartedAt, out.FinishedAt = id, instanceID, node, attempt, reason, at, at
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return SuiteRun{}, err
	}
	if err != nil {
		return SuiteRun{}, fmt.Errorf("store: reuse suite run: %w", err)
	}
	return out, nil
}

// GetSuiteRun returns one attempt or ErrNotFound.
func (db *DB) GetSuiteRun(ctx context.Context, id int64) (SuiteRun, error) {
	return scanSuiteRun(db.sql.QueryRowContext(ctx, `SELECT `+suiteRunColumns+` FROM suite_runs WHERE id=?`, id))
}

// RunningSuiteRun returns the unfinished attempt of the home, or ErrNotFound.
func (db *DB) RunningSuiteRun(ctx context.Context) (SuiteRun, error) {
	return scanSuiteRun(db.sql.QueryRowContext(ctx, `SELECT `+suiteRunColumns+` FROM suite_runs WHERE state='running' LIMIT 1`))
}

func scanSuiteRun(row scanner) (SuiteRun, error) {
	var out SuiteRun
	var started, finished string
	if err := row.Scan(&out.ID, &out.InstanceID, &out.Node, &out.Attempt, &out.Commit, &out.RecipeDigest, &out.TreeBefore, &out.TreeAfter, &out.State, &out.Reason, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SuiteRun{}, ErrNotFound
		}
		return SuiteRun{}, fmt.Errorf("store: read suite run: %w", err)
	}
	var err error
	if out.StartedAt, err = ParseTime(started); err != nil {
		return SuiteRun{}, err
	}
	if finished != "" {
		if out.FinishedAt, err = ParseTime(finished); err != nil {
			return SuiteRun{}, err
		}
	}
	return out, nil
}

func checkSuiteCommand(c SuiteCommand) error {
	if c.RunID < 1 || c.Position < 0 || !validSuiteState(c.State) || c.StartedAt.IsZero() || len(c.OutputTail) > MaxSuiteOutputTail ||
		c.State == SuitePassed && !c.CleanupConfirmed || c.State != SuiteRunning && c.FinishedAt.IsZero() {
		return fmt.Errorf("store: invalid suite command")
	}
	return nil
}

// ClaimSuiteCommand records a command as running, with its process identity,
// before its result is final. Claiming again updates the running row.
func (db *DB) ClaimSuiteCommand(ctx context.Context, c SuiteCommand) error {
	c.State, c.FinishedAt = SuiteRunning, time.Time{}
	if err := checkSuiteCommand(c); err != nil {
		return err
	}
	argv, err := json.Marshal(c.Argv)
	if err != nil {
		return err
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO suite_commands(run_id,position,argv,state,started_at,process_identity) VALUES(?,?,?,'running',?,?)
 ON CONFLICT(run_id,position) DO UPDATE SET process_identity=excluded.process_identity,started_at=excluded.started_at`,
		c.RunID, c.Position, string(argv), FormatTime(c.StartedAt), c.ProcessIdentity)
	if err != nil {
		return fmt.Errorf("store: claim suite command: %w", err)
	}
	return nil
}

// FinishSuiteCommand makes a command final, creating it when it was never
// claimed. A finished command accepts only the same content again; anything
// else is ErrSuiteFinished.
func (db *DB) FinishSuiteCommand(ctx context.Context, c SuiteCommand) error {
	if c.State == SuiteRunning {
		return fmt.Errorf("store: invalid suite command")
	}
	if err := checkSuiteCommand(c); err != nil {
		return err
	}
	argv, err := json.Marshal(c.Argv)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanSuiteCommand(tx.QueryRowContext(ctx, suiteCommandSelect+` WHERE run_id=? AND position=?`, c.RunID, c.Position))
		if errors.Is(err, ErrNotFound) {
			_, err = tx.ExecContext(ctx, `INSERT INTO suite_commands(run_id,position,argv,state,exit_code,reason,started_at,finished_at,process_identity,cleanup_confirmed,output_tail,output_truncated,output_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.RunID, c.Position, string(argv), c.State, c.Exit, c.Reason, FormatTime(c.StartedAt), FormatTime(c.FinishedAt), c.ProcessIdentity, c.CleanupConfirmed, c.OutputTail, c.OutputTruncated, c.OutputDigest)
			return err
		}
		if err != nil {
			return err
		}
		if cur.State != SuiteRunning {
			if cur.State == c.State && cur.Reason == c.Reason && cur.OutputDigest == c.OutputDigest && cur.OutputTail == c.OutputTail && slices.Equal(cur.Argv, c.Argv) && (cur.Exit == nil) == (c.Exit == nil) && (c.Exit == nil || *cur.Exit == *c.Exit) {
				return nil
			}
			return ErrSuiteFinished
		}
		_, err = tx.ExecContext(ctx, `UPDATE suite_commands SET argv=?,state=?,exit_code=?,reason=?,started_at=?,finished_at=?,process_identity=?,cleanup_confirmed=?,output_tail=?,output_truncated=?,output_digest=? WHERE run_id=? AND position=? AND state='running'`,
			string(argv), c.State, c.Exit, c.Reason, FormatTime(c.StartedAt), FormatTime(c.FinishedAt), c.ProcessIdentity, c.CleanupConfirmed, c.OutputTail, c.OutputTruncated, c.OutputDigest, c.RunID, c.Position)
		return err
	})
}

const suiteCommandSelect = `SELECT run_id,position,argv,state,exit_code,reason,started_at,finished_at,process_identity,cleanup_confirmed,output_tail,output_truncated,output_digest FROM suite_commands`

// SuiteCommands returns the commands of one attempt in recipe order.
func (db *DB) SuiteCommands(ctx context.Context, runID int64) ([]SuiteCommand, error) {
	rows, err := db.sql.QueryContext(ctx, suiteCommandSelect+` WHERE run_id=? ORDER BY position`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list suite commands: %w", err)
	}
	defer rows.Close()
	var out []SuiteCommand
	for rows.Next() {
		c, err := scanSuiteCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanSuiteCommand(row scanner) (SuiteCommand, error) {
	var c SuiteCommand
	var argv, started, finished string
	var exit sql.NullInt64
	if err := row.Scan(&c.RunID, &c.Position, &argv, &c.State, &exit, &c.Reason, &started, &finished, &c.ProcessIdentity, &c.CleanupConfirmed, &c.OutputTail, &c.OutputTruncated, &c.OutputDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SuiteCommand{}, ErrNotFound
		}
		return SuiteCommand{}, fmt.Errorf("store: read suite command: %w", err)
	}
	if err := json.Unmarshal([]byte(argv), &c.Argv); err != nil {
		return SuiteCommand{}, fmt.Errorf("store: read suite command: %w", err)
	}
	if exit.Valid {
		v := int(exit.Int64)
		c.Exit = &v
	}
	var err error
	if c.StartedAt, err = ParseTime(started); err != nil {
		return SuiteCommand{}, err
	}
	if finished != "" {
		if c.FinishedAt, err = ParseTime(finished); err != nil {
			return SuiteCommand{}, err
		}
	}
	return c, nil
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

// BugCheckBinding is immutable within one caller-owned test generation.
type BugCheckBinding struct {
	SessionID           int64  `json:"session_id"`
	Operation           string `json:"operation"`
	Generation          string `json:"generation"`
	Repair              string `json:"repair"`
	Phase               string `json:"phase"`
	SourceCommit        string `json:"source_commit"`
	SourceTree          string `json:"source_tree"`
	TargetRepository    string `json:"target_repository"`
	TargetBranch        string `json:"target_branch"`
	TargetCommit        string `json:"target_commit"`
	RecipeDigest        string `json:"recipe_digest"`
	EnvironmentRevision string `json:"environment_revision"`
	PermissionRevision  string `json:"permission_revision"`
}

// BugCheckObservation records command outcome, capture completeness and process cleanup evidence.
type BugCheckObservation struct {
	ExecutableDigest string    `json:"executable_digest"`
	Executable       string    `json:"executable"`
	CWD              string    `json:"cwd"`
	Environment      []string  `json:"environment"`
	Started          time.Time `json:"started"`
	Ended            time.Time `json:"ended"`
	Exit             *int      `json:"exit,omitempty"`
	Outcome          string    `json:"outcome"`
	Reason           string    `json:"reason,omitempty"`
	Output           string    `json:"output"`
	CaptureComplete  bool      `json:"capture_complete"`
	CleanupConfirmed bool      `json:"cleanup_confirmed"`
	ProcessID        int       `json:"process_id,omitempty"`
	ProcessIdentity  string    `json:"process_identity,omitempty"`
}

// BugCheckCommandRecord retains one declared command and its observed execution state.
type BugCheckCommandRecord struct {
	Ordinal     int
	State       string
	Observation BugCheckObservation
}

// BugCheckPhaseRecord retains the immutable phase binding and ordered command evidence.
type BugCheckPhaseRecord struct {
	Binding                        BugCheckBinding
	Recipe                         []byte
	Count                          int
	Outcome, Reason                string
	Cancelled, Cleanup, Unreliable bool
	Current                        bool
	Commands                       []BugCheckCommandRecord
}

func validateBugCheckBinding(b BugCheckBinding) error {
	for _, value := range []string{b.Operation, b.Generation, b.Repair, b.TargetRepository, b.EnvironmentRevision, b.PermissionRevision} {
		if len(value) > 256 {
			return errors.New("bug check binding exceeds bound")
		}
	}
	if b.SessionID <= 0 || b.Operation == "" || len(b.Operation) > 256 || b.Generation == "" || b.Repair == "" || b.TargetRepository == "" || b.TargetBranch != "main" || b.EnvironmentRevision == "" || b.PermissionRevision == "" || !validSubmissionDigest(b.RecipeDigest) {
		return errors.New("invalid Bug check binding")
	}
	for _, v := range []string{b.SourceCommit, b.SourceTree, b.TargetCommit} {
		if len(v) != 40 && len(v) != 64 {
			return errors.New("invalid Bug check source/target identity")
		}
		for _, c := range v {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return errors.New("invalid Bug check object identity")
			}
		}
	}
	switch b.Phase {
	case "regression", "focused", "full":
		return nil
	}
	return errors.New("unknown Bug check phase")
}

// EnsureBugCheckPhase retains a validated complete target recipe and canonical
// input digest. Replay never rewrites earlier evidence or clears reliability.
func (d *DB) EnsureBugCheckPhase(ctx context.Context, b BugCheckBinding, recipe []byte) (BugCheckPhaseRecord, error) {
	if err := validateBugCheckBinding(b); err != nil {
		return BugCheckPhaseRecord{}, err
	}
	r, err := config.ParseBugCheckRecipe(recipe)
	if err != nil {
		return BugCheckPhaseRecord{}, err
	}
	p, err := r.Phase(b.Phase)
	if err != nil {
		return BugCheckPhaseRecord{}, err
	}
	if config.BugRecipeDigest(recipe) != b.RecipeDigest {
		return BugCheckPhaseRecord{}, errors.New("bug recipe digest mismatch")
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return BugCheckPhaseRecord{}, err
	}
	digest := submissionDigest(raw)
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRowContext(ctx, `SELECT input_digest FROM bug_check_phases WHERE session_id=? AND operation=?`, b.SessionID, b.Operation).Scan(&old)
		if e == nil {
			if old != digest {
				return errors.New("bug check replay conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, e = bugIdentityTx(ctx, tx, b.SessionID); e != nil {
			return e
		}
		// Reliability survives fresh generations for the same tested binding.
		var unreliable int
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bug_check_phases WHERE session_id=? AND unreliable=1 AND json_extract(binding,'$.source_commit')=? AND json_extract(binding,'$.target_commit')=? AND json_extract(binding,'$.recipe_digest')=?)`, b.SessionID, b.SourceCommit, b.TargetCommit, b.RecipeDigest).Scan(&unreliable); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO bug_check_phases(session_id,operation,input_digest,binding,recipe,command_count,unreliable) VALUES(?,?,?,?,?,?,?)`, b.SessionID, b.Operation, digest, raw, recipe, len(p.Commands), unreliable)
		return e
	})
	if err != nil {
		return BugCheckPhaseRecord{}, err
	}
	return d.GetBugCheckPhase(ctx, b.SessionID, b.Operation)
}

// GetBugCheckPhase loads a phase by task and durable operation identity.
func (d *DB) GetBugCheckPhase(ctx context.Context, session int64, operation string) (BugCheckPhaseRecord, error) {
	var out BugCheckPhaseRecord
	var raw []byte
	err := d.sql.QueryRowContext(ctx, `SELECT binding,recipe,command_count,outcome,reason,cancelled,cleanup,unreliable,current FROM bug_check_phases WHERE session_id=? AND operation=?`, session, operation).Scan(&raw, &out.Recipe, &out.Count, &out.Outcome, &out.Reason, &out.Cancelled, &out.Cleanup, &out.Unreliable, &out.Current)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Binding); err != nil {
		return out, err
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT ordinal,state,observation FROM bug_check_commands WHERE session_id=? AND operation=? ORDER BY ordinal`, session, operation)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c BugCheckCommandRecord
		var data []byte
		if err = rows.Scan(&c.Ordinal, &c.State, &data); err != nil {
			return out, err
		}
		if len(data) > 0 {
			if err = json.Unmarshal(data, &c.Observation); err != nil {
				return out, err
			}
		}
		out.Commands = append(out.Commands, c)
	}
	return out, rows.Err()
}

// ClaimBugCheckCommand serializes cancellation, phase order and durable intent.
// A false claim is never permission to launch an uncertain command again.
func (d *DB) ClaimBugCheckCommand(ctx context.Context, session int64, operation string, ordinal int) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		var cancelled, unreliable bool
		var outcome string
		if err := tx.QueryRowContext(ctx, `SELECT command_count,cancelled,unreliable,outcome FROM bug_check_phases WHERE session_id=? AND operation=?`, session, operation).Scan(&count, &cancelled, &unreliable, &outcome); err != nil {
			return err
		}
		if ordinal < 0 || ordinal >= count {
			return errors.New("invalid Bug command ordinal")
		}
		if cancelled || unreliable || outcome != "pending" {
			return nil
		}
		var owner, kind string
		if err := tx.QueryRowContext(ctx, `SELECT owner,kind FROM bug_work_guards WHERE session_id=?`, session).Scan(&owner, &kind); err != nil {
			return err
		}
		if owner != operation || kind != "checks" {
			return errors.New("bug command lacks exclusive worktree guard")
		}
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bug_check_commands WHERE session_id=? AND operation=?`, session, operation).Scan(&existing); err != nil {
			return err
		}
		if existing != ordinal {
			return nil
		}
		if ordinal > 0 {
			var state string
			var data []byte
			if err := tx.QueryRowContext(ctx, `SELECT state,observation FROM bug_check_commands WHERE session_id=? AND operation=? AND ordinal=?`, session, operation, ordinal-1).Scan(&state, &data); err != nil {
				return err
			}
			var previous BugCheckObservation
			if err := json.Unmarshal(data, &previous); err != nil {
				return err
			}
			if state != "completed" || previous.Outcome != "passed" || !previous.CleanupConfirmed {
				return nil
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bug_check_commands(session_id,operation,ordinal,state) VALUES(?,?,?,'claimed')`, session, operation, ordinal); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE bug_check_phases SET cleanup=0 WHERE session_id=? AND operation=?`, session, operation)
		claimed = err == nil
		return err
	})
	return claimed, err
}

// ObserveBugCheckCommand records a command observation under its retained phase operation.
func (d *DB) ObserveBugCheckCommand(ctx context.Context, session int64, operation string, ordinal int, finished bool, o BugCheckObservation) error {
	if len(o.Output) > 4<<20 {
		return errors.New("bug command output exceeds bound")
	}
	if finished {
		switch o.Outcome {
		case "passed", "failed", "blocked", "cancelled":
		default:
			return errors.New("invalid Bug command outcome")
		}
		if o.Outcome == "passed" && (o.Exit == nil || *o.Exit != 0 || !o.CaptureComplete || !o.CleanupConfirmed || !validSubmissionDigest(o.ExecutableDigest) || o.ProcessIdentity == "" || o.Started.IsZero() || o.Ended.Before(o.Started)) {
			return errors.New("incomplete Bug passing evidence")
		}
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var state string
		var old []byte
		if err := tx.QueryRowContext(ctx, `SELECT state,observation FROM bug_check_commands WHERE session_id=? AND operation=? AND ordinal=?`, session, operation, ordinal).Scan(&state, &old); err != nil {
			return err
		}
		next := "started"
		if finished {
			next = "completed"
		}
		if state == "completed" {
			if string(old) == string(raw) {
				return nil
			}
			return errors.New("immutable Bug command completion conflict")
		}
		if !finished && state != "claimed" {
			return errors.New("duplicate Bug process observation")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bug_check_events(session_id,operation,kind,content) VALUES(?,?,?,?)`, session, operation, fmt.Sprintf("command_%d_%s", ordinal, next), raw); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bug_check_commands SET state=?,observation=? WHERE session_id=? AND operation=? AND ordinal=?`, next, raw, session, operation, ordinal); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE bug_check_phases SET cleanup=? WHERE session_id=? AND operation=?`, finished && o.CleanupConfirmed, session, operation)
		return err
	})
}

// CancelBugCheckPhase is durable; repeated cancellation cannot start another command.
func (d *DB) CancelBugCheckPhase(ctx context.Context, session int64, operation string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE bug_check_phases SET cancelled=1 WHERE session_id=? AND operation=?`, session, operation)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bug_check_events(session_id,operation,kind,content) VALUES(?,?,'cancel_requested','{}')`, session, operation)
		return err
	})
}

// FinishBugCheckPhase derives passing only from all complete reliable records.
// currentBinding is freshly observed by the factory after the final command.
func (d *DB) FinishBugCheckPhase(ctx context.Context, session int64, operation, outcome, reason string, currentBinding bool) error {
	switch outcome {
	case "passed", "failed", "blocked", "cancelled":
	default:
		return errors.New("invalid Bug phase outcome")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		var cancelled, cleanup, unreliable bool
		var old, oldReason string
		if err := tx.QueryRowContext(ctx, `SELECT command_count,cancelled,cleanup,unreliable,outcome,reason FROM bug_check_phases WHERE session_id=? AND operation=?`, session, operation).Scan(&count, &cancelled, &cleanup, &unreliable, &old, &oldReason); err != nil {
			return err
		}
		if old != "pending" {
			if old == outcome && oldReason == reason {
				return nil
			}
			return errors.New("immutable Bug phase outcome conflict")
		}
		if outcome == "passed" {
			if cancelled || !cleanup || unreliable || !currentBinding {
				return errors.New("bug passing phase has cancelled/stale/unreliable evidence")
			}
			rows, err := tx.QueryContext(ctx, `SELECT state,observation FROM bug_check_commands WHERE session_id=? AND operation=? ORDER BY ordinal`, session, operation)
			if err != nil {
				return err
			}
			found := 0
			for rows.Next() {
				var state string
				var raw []byte
				if err = rows.Scan(&state, &raw); err != nil {
					return errors.Join(err, rows.Close())
				}
				var o BugCheckObservation
				if err = json.Unmarshal(raw, &o); err != nil {
					return errors.Join(err, rows.Close())
				}
				if state != "completed" || o.Outcome != "passed" || o.Exit == nil || *o.Exit != 0 || !o.CaptureComplete || !o.CleanupConfirmed {
					return errors.Join(errors.New("bug phase missing passing command"), rows.Close())
				}
				found++
			}
			err = errors.Join(rows.Err(), rows.Close())
			if err != nil {
				return err
			}
			if found != count {
				return errors.New("bug phase command count mismatch")
			}
		} else if reason == "" {
			return errors.New("nonpassing Bug phase requires reason")
		}
		if outcome == "cancelled" && !cleanup {
			return errors.New("bug cancellation cleanup unconfirmed")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bug_check_phases SET outcome=?,reason=? WHERE session_id=? AND operation=?`, outcome, reason, session, operation); err != nil {
			return err
		}
		raw, err := json.Marshal(struct {
			Outcome string `json:"outcome"`
			Reason  string `json:"reason"`
		}{outcome, reason})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bug_check_events(session_id,operation,kind,content) VALUES(?,?,'settled',?)`, session, operation, raw)
		return err
	})
}

// FlagBugCheckUnreliable permanently prevents this binding being rerun to green.
func (d *DB) FlagBugCheckUnreliable(ctx context.Context, session int64, operation, reason string) error {
	if reason == "" {
		return errors.New("bug reliability flag needs reason")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE bug_check_phases SET unreliable=1,current=0 WHERE session_id=? AND operation=?`, session, operation); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO bug_check_events(session_id,operation,kind,content) VALUES(?,?,'unreliable',?)`, session, operation, []byte(reason))
		return err
	})
}

// InvalidateBugCheckPhase preserves historical results while withholding their
// current use after a source/recipe/target or execution-context observation.
func (d *DB) InvalidateBugCheckPhase(ctx context.Context, session int64, operation, reason string) error {
	if reason == "" {
		return errors.New("bug check invalidation requires reason")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE bug_check_phases SET current=0 WHERE session_id=? AND operation=?`, session, operation); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO bug_check_events(session_id,operation,kind,content) VALUES(?,?,'invalidated',?)`, session, operation, []byte(reason))
		return err
	})
}

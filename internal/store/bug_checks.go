package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	Ordinal     int                 `json:"ordinal"`
	State       string              `json:"state"`
	Observation BugCheckObservation `json:"observation"`
}

// BugCheckPhaseRecord retains the immutable phase binding and ordered command evidence.
type BugCheckPhaseRecord struct {
	Binding                        BugCheckBinding
	Recipe                         []byte
	Count                          int
	Outcome, Reason                string
	Cancelled, Cleanup, Unreliable bool
	Current                        bool
	// Source is "factory" for the trusted final-check recipe phases every
	// final-gate read requires, or "fixer" for a mid-repair self-check a
	// fixer requested; no final-gate read ever trusts a "fixer" record.
	Source   string
	Commands []BugCheckCommandRecord
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
		e := tx.QueryRowContext(ctx, `SELECT input_digest FROM bug_checks WHERE session_id=? AND operation=?`, b.SessionID, b.Operation).Scan(&old)
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
		// Only factory evidence can mark a binding unreliable; a fixer's own
		// self-check flakiness never propagates into the factory's checks.
		var unreliable int
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bug_checks WHERE session_id=? AND source='factory' AND unreliable=1 AND json_extract(binding,'$.source_commit')=? AND json_extract(binding,'$.target_commit')=? AND json_extract(binding,'$.recipe_digest')=?)`, b.SessionID, b.SourceCommit, b.TargetCommit, b.RecipeDigest).Scan(&unreliable); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO bug_checks(session_id,operation,input_digest,binding,recipe,command_count,commands,repair,generation,phase,source,unreliable) VALUES(?,?,?,?,?,?,'[]',?,?,?,'factory',?)`, b.SessionID, b.Operation, digest, raw, recipe, len(p.Commands), b.Repair, b.Generation, b.Phase, unreliable)
		return e
	})
	if err != nil {
		return BugCheckPhaseRecord{}, err
	}
	return d.GetBugCheckPhase(ctx, b.SessionID, b.Operation)
}

func getBugCheckPhaseTx(ctx context.Context, tx *sql.Tx, session int64, operation string) (BugCheckPhaseRecord, error) {
	var out BugCheckPhaseRecord
	var raw, commands []byte
	err := tx.QueryRowContext(ctx, `SELECT binding,recipe,command_count,commands,outcome,reason,cancelled,cleanup,unreliable,current,source FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&raw, &out.Recipe, &out.Count, &commands, &out.Outcome, &out.Reason, &out.Cancelled, &out.Cleanup, &out.Unreliable, &out.Current, &out.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Binding); err != nil {
		return out, err
	}
	if err = json.Unmarshal(commands, &out.Commands); err != nil {
		return out, err
	}
	return out, nil
}

// GetBugCheckPhase loads a phase by task and durable operation identity.
func (d *DB) GetBugCheckPhase(ctx context.Context, session int64, operation string) (BugCheckPhaseRecord, error) {
	var out BugCheckPhaseRecord
	var raw, commands []byte
	err := d.sql.QueryRowContext(ctx, `SELECT binding,recipe,command_count,commands,outcome,reason,cancelled,cleanup,unreliable,current,source FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&raw, &out.Recipe, &out.Count, &commands, &out.Outcome, &out.Reason, &out.Cancelled, &out.Cleanup, &out.Unreliable, &out.Current, &out.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Binding); err != nil {
		return out, err
	}
	err = json.Unmarshal(commands, &out.Commands)
	return out, err
}

// ClaimBugCheckCommand serializes cancellation, phase order and durable intent.
// A false claim is never permission to launch an uncertain command again.
func (d *DB) ClaimBugCheckCommand(ctx context.Context, session int64, operation string, ordinal int) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		var cancelled, unreliable bool
		var outcome string
		var commands []byte
		if err := tx.QueryRowContext(ctx, `SELECT command_count,cancelled,unreliable,outcome,commands FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&count, &cancelled, &unreliable, &outcome, &commands); err != nil {
			return err
		}
		if ordinal < 0 || ordinal >= count {
			return errors.New("invalid Bug command ordinal")
		}
		if cancelled || unreliable || outcome != "pending" {
			return nil
		}
		var owner, kind sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT guard_owner,guard_kind FROM bug_tasks WHERE session_id=?`, session).Scan(&owner, &kind); err != nil {
			return err
		}
		if owner.String != operation || kind.String != "checks" {
			return errors.New("bug command lacks exclusive worktree guard")
		}
		var list []BugCheckCommandRecord
		if err := json.Unmarshal(commands, &list); err != nil {
			return err
		}
		if len(list) != ordinal {
			return nil
		}
		if ordinal > 0 {
			previous := list[ordinal-1]
			if previous.State != "completed" || previous.Observation.Outcome != "passed" || !previous.Observation.CleanupConfirmed {
				return nil
			}
		}
		list = append(list, BugCheckCommandRecord{Ordinal: ordinal, State: "claimed"})
		raw, err := json.Marshal(list)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_checks SET commands=?,cleanup=0 WHERE session_id=? AND operation=?`, raw, session, operation); err != nil {
			return err
		}
		claimed = true
		return nil
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
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var commands []byte
		if err := tx.QueryRowContext(ctx, `SELECT commands FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&commands); err != nil {
			return err
		}
		var list []BugCheckCommandRecord
		if err := json.Unmarshal(commands, &list); err != nil {
			return err
		}
		if ordinal < 0 || ordinal >= len(list) {
			return errors.New("bug command has no retained claim")
		}
		next := "started"
		if finished {
			next = "completed"
		}
		current := list[ordinal]
		if current.State == "completed" {
			if observationEqual(current.Observation, o) {
				return nil
			}
			return errors.New("immutable Bug command completion conflict")
		}
		if !finished && current.State != "claimed" {
			return errors.New("duplicate Bug process observation")
		}
		list[ordinal] = BugCheckCommandRecord{Ordinal: ordinal, State: next, Observation: o}
		raw, err := json.Marshal(list)
		if err != nil {
			return err
		}
		if err = bugEventJSONTx(ctx, tx, session, "check_event", fmt.Sprintf("%s:command_%d_%s", operation, ordinal, next), o); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE bug_checks SET commands=?,cleanup=? WHERE session_id=? AND operation=?`, raw, finished && o.CleanupConfirmed, session, operation)
		return err
	})
}

func observationEqual(a, b BugCheckObservation) bool {
	ja, ea := json.Marshal(a)
	jb, eb := json.Marshal(b)
	return ea == nil && eb == nil && string(ja) == string(jb)
}

// CancelBugCheckPhase is durable; repeated cancellation cannot start another command.
func (d *DB) CancelBugCheckPhase(ctx context.Context, session int64, operation string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE bug_checks SET cancelled=1 WHERE session_id=? AND operation=?`, session, operation)
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
		return bugEventTx(ctx, tx, session, "check_event", operation+":cancel_requested", 0, "", []byte("{}"))
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
		var commands []byte
		if err := tx.QueryRowContext(ctx, `SELECT command_count,cancelled,cleanup,unreliable,outcome,reason,commands FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&count, &cancelled, &cleanup, &unreliable, &old, &oldReason, &commands); err != nil {
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
			var list []BugCheckCommandRecord
			if err := json.Unmarshal(commands, &list); err != nil {
				return err
			}
			found := 0
			for _, c := range list {
				o := c.Observation
				if c.State != "completed" || o.Outcome != "passed" || o.Exit == nil || *o.Exit != 0 || !o.CaptureComplete || !o.CleanupConfirmed {
					return errors.New("bug phase missing passing command")
				}
				found++
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
		if _, err := tx.ExecContext(ctx, `UPDATE bug_checks SET outcome=?,reason=? WHERE session_id=? AND operation=?`, outcome, reason, session, operation); err != nil {
			return err
		}
		return bugEventJSONTx(ctx, tx, session, "check_event", operation+":settled", struct{ Outcome, Reason string }{outcome, reason})
	})
}

// FlagBugCheckUnreliable permanently prevents this binding being rerun to green.
func (d *DB) FlagBugCheckUnreliable(ctx context.Context, session int64, operation, reason string) error {
	if reason == "" {
		return errors.New("bug reliability flag needs reason")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE bug_checks SET unreliable=1,current=0 WHERE session_id=? AND operation=?`, session, operation); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "check_event", operation+":unreliable", 0, "", []byte(bugJSONString(reason)))
	})
}

// InvalidateBugCheckPhase preserves historical results while withholding their
// current use after a source/recipe/target or execution-context observation.
func (d *DB) InvalidateBugCheckPhase(ctx context.Context, session int64, operation, reason string) error {
	if reason == "" {
		return errors.New("bug check invalidation requires reason")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE bug_checks SET current=0 WHERE session_id=? AND operation=?`, session, operation); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "check_event", operation+":invalidated", 0, "", []byte(bugJSONString(reason)))
	})
}

// ReconcileBugCheckStopped records a factory-observed namespace termination.
// It does not overwrite any completed command observation, and cannot create
// passing evidence. The caller owns kernel identity/termination observation.
func (d *DB) ReconcileBugCheckStopped(ctx context.Context, session int64, operation string, ordinal int, identity string, observed time.Time) error {
	if identity == "" || observed.IsZero() {
		return errors.New("check recovery requires process identity and observation")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var commands []byte
		if err := tx.QueryRowContext(ctx, `SELECT commands FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&commands); err != nil {
			return err
		}
		var list []BugCheckCommandRecord
		if err := json.Unmarshal(commands, &list); err != nil {
			return err
		}
		if ordinal < 0 || ordinal >= len(list) {
			return errors.New("check recovery does not name active command")
		}
		old := list[ordinal].Observation
		if old.ProcessIdentity != identity || old.ProcessID <= 0 || old.Started.IsZero() || observed.Before(old.Started) {
			return errors.New("check recovery process identity mismatch")
		}
		var cancelled, cleanup bool
		if err := tx.QueryRowContext(ctx, `SELECT cancelled,cleanup FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&cancelled, &cleanup); err != nil {
			return err
		}
		if cleanup {
			return nil
		}
		if ordinal != len(list)-1 {
			return errors.New("check recovery does not name active command")
		}
		state := list[ordinal].State
		if state != "started" && state != "completed" {
			return errors.New("check recovery lacks retained launch observation")
		}
		proof, err := json.Marshal(struct {
			Identity string    `json:"identity"`
			Observed time.Time `json:"observed"`
			Ordinal  int       `json:"ordinal"`
		}{identity, observed, ordinal})
		if err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "check_event", operation+":namespace_stopped", 0, "", proof); err != nil {
			return err
		}
		if state != "completed" {
			old.Outcome = "blocked"
			old.Reason = "check interrupted; namespace termination confirmed during recovery"
			if cancelled {
				old.Outcome = "cancelled"
				old.Reason = "check cancellation termination confirmed during recovery"
			}
			old.Ended = observed
			old.Exit = nil
			old.CaptureComplete = false
			old.CleanupConfirmed = true
			list[ordinal] = BugCheckCommandRecord{Ordinal: ordinal, State: "completed", Observation: old}
			raw, err := json.Marshal(list)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE bug_checks SET commands=? WHERE session_id=? AND operation=?`, raw, session, operation); err != nil {
				return err
			}
		}
		// Completed observations remain immutable even if their cleanup was unknown.
		// The append-only namespace_stopped event supplies later cleanup evidence.
		_, err = tx.ExecContext(ctx, `UPDATE bug_checks SET cleanup=1 WHERE session_id=? AND operation=?`, session, operation)
		return err
	})
}

// ReleaseQuiescentBugCheckWork releases only this phase's check guard and is
// idempotent after recovery. Foreign or child ownership is never removed.
func (d *DB) ReleaseQuiescentBugCheckWork(ctx context.Context, session int64, operation string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var cleanup bool
		if err := tx.QueryRowContext(ctx, `SELECT cleanup FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&cleanup); err != nil {
			return err
		}
		if !cleanup {
			return errors.New("check cleanup remains unresolved")
		}
		return releaseBugWorkOwnerTx(ctx, tx, session, operation)
	})
}

// BugFailureReason is one prior failed attempt's retained reason, read from
// bug_checks and bug_events instead of the removed bug_execution_sections
// table (AC-008).
type BugFailureReason struct {
	Reference, Phase, Outcome, Reason string
	Evidence                          []string
}

// bugFailureEventContent is one bug_events row of kind "failure": a reason
// plus its supporting evidence references, replacing UpdateBugFailureReferences.
type bugFailureEventContent struct {
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence,omitempty"`
}

// RecordBugFailure appends a durable failure reason as a bug_events row (kind
// "failure"), replacing the removed UpdateBugFailureReferences. reference is
// the caller's own durable operation identity (idempotent-replay key).
func (d *DB) RecordBugFailure(ctx context.Context, session, expected int64, reference, reason string, evidence []string) error {
	if session <= 0 || expected <= 0 || reference == "" || len(reference) > 256 || strings.TrimSpace(reason) == "" || len(reason) > 65536 {
		return errors.New("store: invalid Bug failure record")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := bugEventByReferenceTx(ctx, tx, session, "failure", reference)
		if err == nil {
			var old bugFailureEventContent
			if err = json.Unmarshal(existing, &old); err != nil {
				return err
			}
			if old.Reason != reason {
				return errors.New("store: Bug failure replay conflict")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		state, err := bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		if err = bugEventJSONTx(ctx, tx, session, "failure", reference, bugFailureEventContent{Reason: reason, Evidence: evidence}); err != nil {
			return err
		}
		return bugBumpRevisionTx(ctx, tx, session)
	})
}

// BugFailureReasons returns why prior check attempts and setup steps for this
// bug failed, reading bug_checks phase outcomes joined with the failure events
// any setup step recorded. It replaces the removed
// ReadBugExecutionSections/BugFailureReferences section as the fixer's one
// source of "why did it fail before" (AC-008).
func (d *DB) BugFailureReasons(ctx context.Context, session int64) ([]BugFailureReason, error) {
	var out []BugFailureReason
	rows, err := d.sql.QueryContext(ctx, `SELECT operation,phase,outcome,reason FROM bug_checks WHERE session_id=? AND source='factory' AND outcome NOT IN ('pending','passed') ORDER BY operation`, session)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r BugFailureReason
		if err = rows.Scan(&r.Reference, &r.Phase, &r.Outcome, &r.Reason); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	events, err := d.sql.QueryContext(ctx, `SELECT reference,content FROM bug_events WHERE session_id=? AND kind='failure' ORDER BY id`, session)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	for events.Next() {
		var reference string
		var content []byte
		if err = events.Scan(&reference, &content); err != nil {
			return nil, err
		}
		var f bugFailureEventContent
		if err = json.Unmarshal(content, &f); err != nil {
			return nil, err
		}
		out = append(out, BugFailureReason{Reference: reference, Outcome: "failure", Reason: f.Reason, Evidence: f.Evidence})
	}
	return out, events.Err()
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// BugRerunOutputLimit bounds each retained rerun output; the tail is kept,
// where go test prints its failures and package summaries.
const BugRerunOutputLimit = 256 << 10

// BugCheckTest names one top-level test in its package.
type BugCheckTest struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

// BugCheckRerun is one factory rerun of a failed command's failing tests,
// on the repair worktree ("repair") or on the target commit ("target").
type BugCheckRerun struct {
	Target          string              `json:"target"`
	State           string              `json:"state"`
	Tests           []BugCheckTest      `json:"tests"`
	Observation     BugCheckObservation `json:"observation"`
	OutputTruncated bool                `json:"output_truncated,omitempty"`
}

// BugCheckAttribution retains why a failed command's tests failed. The failed
// command observation itself is never rewritten; Outcome "passed" means every
// failing test was flaky or also failed on the target, so the command counts
// as passed with those known failures shown in the review.
type BugCheckAttribution struct {
	Reruns      []BugCheckRerun `json:"reruns"`
	Outcome     string          `json:"outcome,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	Flaky       []BugCheckTest  `json:"flaky,omitempty"`
	PreExisting []BugCheckTest  `json:"pre_existing,omitempty"`
	Repair      []BugCheckTest  `json:"repair,omitempty"`
}

// Passed reports whether the command counts as passing: either it passed, or
// it failed only with tests whose rerun attribution showed them unrelated to
// the repair, with every rerun completed and cleaned up.
func (c BugCheckCommandRecord) Passed() bool {
	o := c.Observation
	if c.State != "completed" || o.Exit == nil || !o.CaptureComplete || !o.CleanupConfirmed {
		return false
	}
	if o.Outcome == "passed" && *o.Exit == 0 {
		return true
	}
	a := c.Attribution
	if o.Outcome != "failed" || *o.Exit != 1 || a == nil || a.Outcome != "passed" || len(a.Repair) != 0 || len(a.Flaky)+len(a.PreExisting) == 0 || len(a.Reruns) == 0 {
		return false
	}
	for _, r := range a.Reruns {
		if r.State != "completed" || !r.Observation.CleanupConfirmed {
			return false
		}
	}
	return true
}

// LaunchIdentity names the process recovery must observe: an unfinished
// rerun's process, otherwise the command's own.
func (c BugCheckCommandRecord) LaunchIdentity() string {
	if r := c.activeRerun(); r != nil {
		return r.Observation.ProcessIdentity
	}
	return c.Observation.ProcessIdentity
}

// RerunActive reports a claimed or started rerun not yet observed to end.
func (c BugCheckCommandRecord) RerunActive() bool { return c.activeRerun() != nil }

func (c BugCheckCommandRecord) activeRerun() *BugCheckRerun {
	if c.Attribution == nil || len(c.Attribution.Reruns) == 0 {
		return nil
	}
	last := &c.Attribution.Reruns[len(c.Attribution.Reruns)-1]
	if last.State == "completed" {
		return nil
	}
	return last
}

// attributableTx loads the phase's last command when it is a completed
// ordinary test failure still open to attribution under this check's guard.
// halted reports a cancelled or unreliable phase, which may launch nothing
// more; a rerun already running is still observed to its end.
func attributableTx(ctx context.Context, tx *sql.Tx, session int64, operation string, ordinal int) (list []BugCheckCommandRecord, halted bool, err error) {
	var commands []byte
	var cancelled, unreliable bool
	var outcome string
	if err = tx.QueryRowContext(ctx, `SELECT commands,cancelled,unreliable,outcome FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&commands, &cancelled, &unreliable, &outcome); err != nil {
		return nil, false, err
	}
	if outcome != "pending" {
		return nil, false, errors.New("bug rerun phase is no longer pending")
	}
	var owner, kind sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT guard_owner,guard_kind FROM bug_tasks WHERE session_id=?`, session).Scan(&owner, &kind); err != nil {
		return nil, false, err
	}
	if owner.String != operation || kind.String != "checks" {
		return nil, false, errors.New("bug rerun lacks exclusive worktree guard")
	}
	if err = json.Unmarshal(commands, &list); err != nil {
		return nil, false, err
	}
	if ordinal < 0 || ordinal != len(list)-1 {
		return nil, false, errors.New("bug rerun does not name the last command")
	}
	c := list[ordinal]
	o := c.Observation
	if c.State != "completed" || o.Outcome != "failed" || o.Exit == nil || *o.Exit != 1 || !o.CaptureComplete || !o.CleanupConfirmed {
		return nil, false, errors.New("bug rerun requires a completed ordinary test failure")
	}
	if c.Attribution != nil && c.Attribution.Outcome != "" {
		return nil, false, errors.New("bug command attribution is already settled")
	}
	return list, cancelled || unreliable, nil
}

func saveBugCheckCommandsTx(ctx context.Context, tx *sql.Tx, session int64, operation string, list []BugCheckCommandRecord, cleanup bool) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE bug_checks SET commands=?,cleanup=? WHERE session_id=? AND operation=?`, raw, cleanup, session, operation)
	return err
}

// ClaimBugCheckRerun durably records the intent to rerun named tests before
// launch: the repair rerun first, then at most one target rerun. A false claim
// is never permission to launch.
func (d *DB) ClaimBugCheckRerun(ctx context.Context, session int64, operation string, ordinal int, target string, tests []BugCheckTest) (bool, error) {
	if len(tests) == 0 || len(tests) > 256 {
		return false, errors.New("bug rerun needs 1..256 tests")
	}
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		list, halted, err := attributableTx(ctx, tx, session, operation, ordinal)
		if err != nil || halted {
			return err
		}
		a := list[ordinal].Attribution
		if a == nil {
			a = &BugCheckAttribution{}
		}
		want := []string{"repair", "target"}
		if len(a.Reruns) >= len(want) || want[len(a.Reruns)] != target || list[ordinal].activeRerun() != nil {
			return nil
		}
		a.Reruns = append(a.Reruns, BugCheckRerun{Target: target, State: "claimed", Tests: slices.Clone(tests)})
		list[ordinal].Attribution = a
		if err = bugEventJSONTx(ctx, tx, session, "check_event", fmt.Sprintf("%s:command_%d_rerun_%s_claimed", operation, ordinal, target), tests); err != nil {
			return err
		}
		claimed = true
		return saveBugCheckCommandsTx(ctx, tx, session, operation, list, false)
	})
	return claimed, err
}

// ObserveBugCheckRerun records the claimed rerun's launch identity and then
// its completion, exactly as ObserveBugCheckCommand does for a command. The
// output is retained up to BugRerunOutputLimit, keeping its tail.
func (d *DB) ObserveBugCheckRerun(ctx context.Context, session int64, operation string, ordinal int, finished bool, o BugCheckObservation) error {
	if finished {
		switch o.Outcome {
		case "passed", "failed", "blocked", "cancelled":
		default:
			return errors.New("invalid Bug rerun outcome")
		}
	}
	truncated := false
	if len(o.Output) > BugRerunOutputLimit {
		o.Output, truncated = o.Output[len(o.Output)-BugRerunOutputLimit:], true
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		list, _, err := attributableTx(ctx, tx, session, operation, ordinal)
		if err != nil {
			return err
		}
		r := list[ordinal].activeRerun()
		if r == nil {
			return errors.New("bug rerun has no retained claim")
		}
		if !finished && r.State != "claimed" {
			return errors.New("duplicate Bug rerun process observation")
		}
		next := "started"
		if finished {
			next = "completed"
		}
		r.State, r.Observation, r.OutputTruncated = next, o, truncated
		if err = bugEventJSONTx(ctx, tx, session, "check_event", fmt.Sprintf("%s:command_%d_rerun_%s_%s", operation, ordinal, r.Target, next), o); err != nil {
			return err
		}
		return saveBugCheckCommandsTx(ctx, tx, session, operation, list, finished && o.CleanupConfirmed)
	})
}

// AttributeBugCheckCommand settles a failed command's attribution once. A
// passing attribution needs no repair failure, at least one known failure and
// completed, cleaned-up reruns; anything else retains the command's failure.
func (d *DB) AttributeBugCheckCommand(ctx context.Context, session int64, operation string, ordinal int, verdict BugCheckAttribution) error {
	if (verdict.Outcome != "passed" && verdict.Outcome != "failed") || verdict.Reason == "" || len(verdict.Reason) > 65536 {
		return errors.New("invalid Bug command attribution")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		list, _, err := attributableTx(ctx, tx, session, operation, ordinal)
		if err != nil {
			return err
		}
		a := list[ordinal].Attribution
		if a == nil {
			a = &BugCheckAttribution{}
		}
		if list[ordinal].activeRerun() != nil {
			return errors.New("bug rerun remains unresolved")
		}
		a.Outcome, a.Reason = verdict.Outcome, verdict.Reason
		a.Flaky, a.PreExisting, a.Repair = slices.Clone(verdict.Flaky), slices.Clone(verdict.PreExisting), slices.Clone(verdict.Repair)
		list[ordinal].Attribution = a
		if a.Outcome == "passed" && !list[ordinal].Passed() {
			return errors.New("bug passing attribution lacks complete rerun evidence")
		}
		if err = bugEventJSONTx(ctx, tx, session, "check_event", fmt.Sprintf("%s:command_%d_attributed", operation, ordinal), struct{ Outcome, Reason string }{a.Outcome, a.Reason}); err != nil {
			return err
		}
		cleanup := true
		for _, r := range a.Reruns {
			cleanup = cleanup && r.Observation.CleanupConfirmed
		}
		return saveBugCheckCommandsTx(ctx, tx, session, operation, list, cleanup)
	})
}

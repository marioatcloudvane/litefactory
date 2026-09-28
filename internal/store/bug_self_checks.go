package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// BugSelfCheckMaxRuns bounds fixer-requested self-checks per repair attempt
// (AC-004). One run is regression, then focused only if regression passed; the
// full phase can never be self-checked.
const BugSelfCheckMaxRuns = 5

// BugSelfCheckStart reports bug_self_check_start's immediate durable outcome:
// "started" (a fresh regression or continuing focused phase was allocated),
// "pending" (an existing run is still unresolved; Operation/Phase are empty
// because nothing new was allocated) or "limit_reached" (Count holds the
// number of runs already spent).
type BugSelfCheckStart struct {
	Status             string
	RunID, Operation   string
	Phase              string
	Ordinal, Remaining int
	Count              int
}

// BugSelfCheckRun aggregates one self-check run's phase rows (1 while
// regression is unresolved or failed, 2 once focused was added).
type BugSelfCheckRun struct {
	RunID   string
	Ordinal int
	Phases  []BugCheckPhaseRecord
}

// Done reports whether this run has reached a terminal state: its regression
// phase resolved to anything but passed, or its focused phase (added only
// after regression passed) has itself resolved. A regression pass with no
// focused phase yet is not done; the run's own background execution is
// expected to add it next.
func (r BugSelfCheckRun) Done() bool {
	var regressionOutcome string
	var hasFocused bool
	var focusedOutcome string
	for _, phase := range r.Phases {
		switch phase.Binding.Phase {
		case "regression":
			regressionOutcome = phase.Outcome
		case "focused":
			hasFocused = true
			focusedOutcome = phase.Outcome
		}
	}
	if regressionOutcome == "" || regressionOutcome == "pending" {
		return false
	}
	if regressionOutcome != "passed" {
		return true
	}
	if !hasFocused {
		return false
	}
	return focusedOutcome != "pending"
}

// BugSelfCheckPhaseSummary is one phase's review-facing outcome, without its
// full command evidence.
type BugSelfCheckPhaseSummary struct {
	Phase, Outcome, Reason string
}

// BugSelfCheckSummary is one run's review-facing evidence: enough to render
// the delivery review's evidence line and detect staleness, without carrying
// every command's full captured output into every digest computation.
type BugSelfCheckSummary struct {
	RunID   string
	Ordinal int
	Phases  []BugSelfCheckPhaseSummary
}

// SummarizeBugSelfChecks projects full self-check runs into their bounded,
// digest- and review-facing summary, ordered by ordinal.
func SummarizeBugSelfChecks(runs []BugSelfCheckRun) []BugSelfCheckSummary {
	out := make([]BugSelfCheckSummary, 0, len(runs))
	for _, run := range runs {
		s := BugSelfCheckSummary{RunID: run.RunID, Ordinal: run.Ordinal}
		for _, phase := range run.Phases {
			s.Phases = append(s.Phases, BugSelfCheckPhaseSummary{Phase: phase.Binding.Phase, Outcome: phase.Outcome, Reason: phase.Reason})
		}
		out = append(out, s)
	}
	return out
}

func selfCheckOperation(repair string, ordinal int, phase string) string {
	return fmt.Sprintf("selfcheck:%s:%d:%s", repair, ordinal, phase)
}

// selfCheckRunID identifies one self-check run to the fixer, independent of
// its individual phase rows.
func selfCheckRunID(repair string, ordinal int) string {
	return fmt.Sprintf("selfcheck:%s:%d", repair, ordinal)
}

// StartBugSelfCheck allocates or continues one self-check run's phase for a
// dispatched repair attempt, running only under that repair's own worktree
// guard and taking none of its own (AC-007). It never reserves, counts
// against, releases or otherwise touches the three-attempt repair budget, and
// writes no bug_repairs state (AC-005).
//
// ordinal is 0 to start a fresh run's regression phase: this allocates the
// next ordinal (or, unchanged, returns an already pending run's identity,
// honoring "starting again while a run is pending returns that same pending
// run"), or reports limit_reached once BugSelfCheckMaxRuns runs already exist.
// ordinal is an existing run's own ordinal to add its focused phase, only once
// that run's regression phase has passed and only once.
//
// binding.Phase must be "regression" (with ordinal 0) or "focused" (with a
// nonzero ordinal); the full phase can never be self-checked (AC-002).
// commandCount is the phase's parsed command count, already known to the
// caller, which read and parsed the recipe before calling this method.
func (d *DB) StartBugSelfCheck(ctx context.Context, session int64, repair string, ordinal int, binding BugCheckBinding, recipe []byte, commandCount int) (BugSelfCheckStart, error) {
	if ordinal < 0 || ordinal > BugSelfCheckMaxRuns {
		return BugSelfCheckStart{}, errors.New("invalid Bug self-check ordinal")
	}
	if (ordinal == 0) != (binding.Phase == "regression") {
		return BugSelfCheckStart{}, errors.New("bug self-check ordinal/phase mismatch")
	}
	if binding.Phase != "regression" && binding.Phase != "focused" {
		return BugSelfCheckStart{}, errors.New("bug self-check refuses the full phase")
	}
	if binding.Repair != repair {
		return BugSelfCheckStart{}, errors.New("bug self-check binding repair mismatch")
	}
	if commandCount < 1 || commandCount > 64 {
		return BugSelfCheckStart{}, errors.New("invalid Bug self-check command count")
	}
	if config.BugRecipeDigest(recipe) != binding.RecipeDigest {
		return BugSelfCheckStart{}, errors.New("bug self-check recipe digest mismatch")
	}
	var out BugSelfCheckStart
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, session, repair)
		if err != nil {
			return err
		}
		if r.State != "dispatched" {
			return errors.New("bug self-check requires a dispatched repair attempt")
		}
		var owner, kind sql.NullString
		var cancelled bool
		if err := tx.QueryRowContext(ctx, `SELECT guard_owner,guard_kind,cancelled FROM bug_tasks WHERE session_id=?`, session).Scan(&owner, &kind, &cancelled); err != nil {
			return err
		}
		if cancelled {
			return errors.New("bug is cancelled")
		}
		if owner.String != "repair:"+repair || kind.String != "repair" {
			return errors.New("bug self-check requires the repair's own worktree guard")
		}
		rows, err := tx.QueryContext(ctx, `SELECT generation,phase,outcome FROM bug_checks WHERE session_id=? AND repair=? AND source='fixer'`, session, repair)
		if err != nil {
			return err
		}
		type existingRow struct{ generation, phase, outcome string }
		var existing []existingRow
		for rows.Next() {
			var er existingRow
			if err := rows.Scan(&er.generation, &er.phase, &er.outcome); err != nil {
				_ = rows.Close()
				return err
			}
			existing = append(existing, er)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		runPhases := map[int]map[string]string{}
		for _, er := range existing {
			n, convErr := strconv.Atoi(er.generation)
			if convErr != nil {
				return convErr
			}
			if runPhases[n] == nil {
				runPhases[n] = map[string]string{}
			}
			runPhases[n][er.phase] = er.outcome
		}
		count := len(runPhases)
		pendingOrdinal := 0
		for n, phases := range runPhases {
			regressionOutcome, hasRegression := phases["regression"]
			if !hasRegression {
				continue
			}
			if regressionOutcome == "pending" {
				pendingOrdinal = n
				break
			}
			if regressionOutcome == "passed" {
				focusedOutcome, hasFocused := phases["focused"]
				if !hasFocused || focusedOutcome == "pending" {
					pendingOrdinal = n
					break
				}
			}
		}
		if ordinal == 0 {
			if pendingOrdinal != 0 {
				out = BugSelfCheckStart{Status: "pending", RunID: selfCheckRunID(repair, pendingOrdinal), Ordinal: pendingOrdinal, Remaining: BugSelfCheckMaxRuns - count}
				return nil
			}
			if count >= BugSelfCheckMaxRuns {
				out = BugSelfCheckStart{Status: "limit_reached", Count: count}
				return nil
			}
			ordinal = count + 1
		} else {
			phases, ok := runPhases[ordinal]
			if !ok {
				return errors.New("bug self-check run does not exist")
			}
			if phases["regression"] != "passed" {
				return errors.New("bug self-check focused phase requires a passed regression phase")
			}
			if _, exists := phases["focused"]; exists {
				return errors.New("bug self-check focused phase already exists")
			}
		}
		fullBinding := binding
		fullBinding.SessionID = session
		fullBinding.Generation = strconv.Itoa(ordinal)
		fullBinding.Operation = selfCheckOperation(repair, ordinal, binding.Phase)
		// Operation and Generation are only known once ordinal is resolved above,
		// so the complete binding is validated here, not on the caller's partial
		// input before this transaction.
		if err := validateBugCheckBinding(fullBinding); err != nil {
			return err
		}
		raw, err := json.Marshal(fullBinding)
		if err != nil {
			return err
		}
		digest := submissionDigest(raw)
		if _, err = bugIdentityTx(ctx, tx, session); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_checks(session_id,operation,input_digest,binding,recipe,command_count,commands,repair,generation,phase,source) VALUES(?,?,?,?,?,?,'[]',?,?,?,'fixer')`, session, fullBinding.Operation, digest, raw, recipe, commandCount, repair, fullBinding.Generation, fullBinding.Phase); err != nil {
			return err
		}
		if err = bugEventJSONTx(ctx, tx, session, "self_check_event", fullBinding.Operation+":started", fullBinding); err != nil {
			return err
		}
		out = BugSelfCheckStart{Status: "started", RunID: selfCheckRunID(repair, ordinal), Operation: fullBinding.Operation, Phase: fullBinding.Phase, Ordinal: ordinal, Remaining: BugSelfCheckMaxRuns - ordinal}
		return nil
	})
	return out, err
}

// FinishBugSelfCheckPhase records one self-check phase's complete outcome and
// per-command evidence in a single write; unlike the factory's checks, a
// self-check phase is not claimed and observed command by command, because it
// runs under the repair's own guard rather than one of its own and is always
// advisory. A run a crash leaves with no observed result stays "pending" —
// uncertain in effect — until restart recovery marks it "cancelled" through
// StopBugSelfChecks/PendingBugSelfCheckRepairs (AC-011); this method never
// writes that transition itself.
func (d *DB) FinishBugSelfCheckPhase(ctx context.Context, session int64, operation, outcome, reason string, commands []BugCheckCommandRecord) error {
	switch outcome {
	case "passed", "failed", "blocked", "cancelled":
	default:
		return errors.New("invalid Bug self-check phase outcome")
	}
	if outcome != "passed" && strings.TrimSpace(reason) == "" {
		return errors.New("nonpassing Bug self-check phase requires reason")
	}
	raw, err := json.Marshal(commands)
	if err != nil {
		return err
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var source, old string
		err := tx.QueryRowContext(ctx, `SELECT source,outcome FROM bug_checks WHERE session_id=? AND operation=?`, session, operation).Scan(&source, &old)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if source != "fixer" {
			return errors.New("bug self-check finish requires a fixer-owned row")
		}
		if old != "pending" {
			if old == outcome {
				return nil
			}
			return errors.New("immutable Bug self-check phase outcome conflict")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bug_checks SET outcome=?,reason=?,commands=? WHERE session_id=? AND operation=?`, outcome, reason, raw, session, operation); err != nil {
			return err
		}
		return bugEventJSONTx(ctx, tx, session, "self_check_event", operation+":settled", struct{ Outcome, Reason string }{outcome, reason})
	})
}

// bugSelfChecksTx reads every self-check run recorded for a repair attempt,
// ordinal ascending, inside the caller's own transaction.
func bugSelfChecksTx(ctx context.Context, tx *sql.Tx, session int64, repair string) ([]BugSelfCheckRun, error) {
	rows, err := tx.QueryContext(ctx, `SELECT operation,generation FROM bug_checks WHERE session_id=? AND repair=? AND source='fixer' ORDER BY generation,phase`, session, repair)
	if err != nil {
		return nil, err
	}
	type ref struct{ operation, generation string }
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.operation, &r.generation); err != nil {
			_ = rows.Close()
			return nil, err
		}
		refs = append(refs, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	byOrdinal := map[int]*BugSelfCheckRun{}
	var order []int
	for _, r := range refs {
		n, err := strconv.Atoi(r.generation)
		if err != nil {
			return nil, err
		}
		phase, err := getBugCheckPhaseTx(ctx, tx, session, r.operation)
		if err != nil {
			return nil, err
		}
		run, ok := byOrdinal[n]
		if !ok {
			run = &BugSelfCheckRun{RunID: selfCheckRunID(repair, n), Ordinal: n}
			byOrdinal[n] = run
			order = append(order, n)
		}
		run.Phases = append(run.Phases, phase)
	}
	sort.Ints(order)
	out := make([]BugSelfCheckRun, 0, len(order))
	for _, n := range order {
		out = append(out, *byOrdinal[n])
	}
	return out, nil
}

// BugSelfChecks reads every self-check run recorded for a repair attempt
// (session, request), ordinal ascending. It is the one read the delivery
// review's evidence line and bug_self_check_status both use.
func (d *DB) BugSelfChecks(ctx context.Context, session int64, repair string) ([]BugSelfCheckRun, error) {
	var out []BugSelfCheckRun
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugSelfChecksTx(ctx, tx, session, repair)
		return e
	})
	return out, err
}

// StopBugSelfChecks marks every source=fixer bug_checks row for this repair
// still pending as cancelled. It is idempotent, takes no process observation
// of its own (the caller stops any locally running command and waits for its
// process group first) and never fails just because nothing was pending. It
// serves two callers: repair completion, which calls it before saving the
// repair's source (AC-007), and restart recovery, which calls it for every
// repair with a run a crash left unresolved, so that run is never itself
// rerun (AC-011).
func (d *DB) StopBugSelfChecks(ctx context.Context, session int64, repair string) (int, error) {
	var count int
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT operation FROM bug_checks WHERE session_id=? AND repair=? AND source='fixer' AND outcome='pending'`, session, repair)
		if err != nil {
			return err
		}
		var operations []string
		for rows.Next() {
			var op string
			if err := rows.Scan(&op); err != nil {
				_ = rows.Close()
				return err
			}
			operations = append(operations, op)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, op := range operations {
			if _, err := tx.ExecContext(ctx, `UPDATE bug_checks SET outcome='cancelled',reason='self-check stopped before repair completion' WHERE session_id=? AND operation=?`, session, op); err != nil {
				return err
			}
			if err := bugEventTx(ctx, tx, session, "self_check_event", op+":cancelled", 0, "", []byte(`"self-check stopped before repair completion"`)); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// BugSelfCheckPendingRepair names one repair attempt with a self-check run a
// crash left unresolved, for restart recovery to cancel (AC-011).
type BugSelfCheckPendingRepair struct {
	SessionID int64
	Repair    string
}

// PendingBugSelfCheckRepairs lists every (session, repair) pair with a
// source=fixer bug_checks row still pending, across every bug. Restart
// recovery cancels each through StopBugSelfChecks without rerunning it.
func (d *DB) PendingBugSelfCheckRepairs(ctx context.Context) ([]BugSelfCheckPendingRepair, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT DISTINCT session_id,repair FROM bug_checks WHERE source='fixer' AND outcome='pending' AND repair IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugSelfCheckPendingRepair
	for rows.Next() {
		var p BugSelfCheckPendingRepair
		if err := rows.Scan(&p.SessionID, &p.Repair); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

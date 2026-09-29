package store

import (
	"testing"
)

// bugSelfCheckFixture returns a session with an approved analysis, a
// dispatched repair attempt (the repair's own worktree guard held), ready for
// StartBugSelfCheck.
func bugSelfCheckFixture(t *testing.T, issue int) (db *DB, session Session, request string) {
	t.Helper()
	db = newTestDB(t)
	var identity BugIdentity
	session, identity = bugFixtureSession(t, db, issue)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	analystRun := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	analystAttempt := bugFixtureAttempt(t, db, instance, "analyst", analystRun.ID, bugFixtureTime(0))
	revision, approval := bugFixtureApprovedAnalysis(t, db, session, identity, instance, analystAttempt, analystRun, bugFixtureTime(0))

	// The fixer's own run must already exist and be bound to the "attempt"
	// graph attempt before ClaimBugRepairDispatch will recognize it.
	fixerRun := bugFixtureRun(t, db, session, 2, bugFixtureTime(0))
	attempt := bugFixtureAttempt(t, db, instance, "attempt", fixerRun.ID, bugFixtureTime(0))

	request = "repair-1"
	in := bugFixtureRepairRequest(session, revision, approval, request, "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatalf("reserve repair: %v", err)
	}
	if claimed, err := db.ClaimBugRepairDispatch(t.Context(), session.ID, request, in.SourceCommit, in.SourceTree, attempt.ID, fixerRun.ID); err != nil || !claimed {
		t.Fatalf("claim repair dispatch: claimed=%v err=%v", claimed, err)
	}
	return db, session, request
}

// TestStartBugSelfCheck_PhaseSequencingAndCap is AC-002/AC-004: a fresh run
// starts on regression, the focused phase can only be added after regression
// passes, and the fifth run reports limit_reached rather than a sixth start.
func TestStartBugSelfCheck_PhaseSequencingAndCap(t *testing.T) {
	t.Parallel()
	db, session, request := bugSelfCheckFixture(t, 440)
	recipe := bugFixtureCheckRecipe()

	// The full phase can never be self-checked.
	fullBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "full")
	if _, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, fullBinding, recipe, 1); err == nil {
		t.Fatal("the full phase was accepted for a self-check")
	}

	regressionBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "regression")
	start, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
	if err != nil {
		t.Fatalf("start regression: %v", err)
	}
	if start.Status != "started" || start.Phase != "regression" || start.Ordinal != 1 {
		t.Fatalf("start = %+v", start)
	}

	// Focused cannot be added before regression resolves.
	focusedBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "focused")
	if _, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 1, focusedBinding, recipe, 1); err == nil {
		t.Fatal("focused phase started before regression resolved")
	}

	// Starting again with ordinal 0 while a run is pending returns that same
	// pending run rather than allocating a new one.
	again, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
	if err != nil {
		t.Fatalf("pending re-start: %v", err)
	}
	if again.Status != "pending" || again.Ordinal != 1 {
		t.Fatalf("pending re-start = %+v", again)
	}

	if err := db.FinishBugSelfCheckPhase(t.Context(), session.ID, start.Operation, "passed", "", nil); err != nil {
		t.Fatalf("finish regression: %v", err)
	}

	focused, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 1, focusedBinding, recipe, 1)
	if err != nil {
		t.Fatalf("start focused: %v", err)
	}
	if focused.Status != "started" || focused.Phase != "focused" || focused.Ordinal != 1 {
		t.Fatalf("focused start = %+v", focused)
	}
	if err := db.FinishBugSelfCheckPhase(t.Context(), session.ID, focused.Operation, "passed", "", nil); err != nil {
		t.Fatalf("finish focused: %v", err)
	}

	runs, err := db.BugSelfChecks(t.Context(), session.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || len(runs[0].Phases) != 2 || !runs[0].Done() {
		t.Fatalf("runs = %+v", runs)
	}

	// Spend runs 2..5, each failing regression outright so a fresh ordinal is
	// always allocated on the next ordinal-0 start.
	for i := 2; i <= BugSelfCheckMaxRuns; i++ {
		s, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
		if err != nil {
			t.Fatalf("start run %d: %v", i, err)
		}
		if s.Status != "started" || s.Ordinal != i {
			t.Fatalf("run %d start = %+v", i, s)
		}
		if err := db.FinishBugSelfCheckPhase(t.Context(), session.ID, s.Operation, "failed", "flaky", nil); err != nil {
			t.Fatalf("finish run %d: %v", i, err)
		}
	}

	limited, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
	if err != nil {
		t.Fatalf("limited start: %v", err)
	}
	if limited.Status != "limit_reached" || limited.Count != BugSelfCheckMaxRuns {
		t.Fatalf("limited start = %+v, want limit_reached at %d", limited, BugSelfCheckMaxRuns)
	}
}

// TestBugSelfCheck_SourceNeverSatisfiesAFinalGate is the source-filtering
// requirement: a fixer self-check pass, however clean, must never be
// indistinguishable from a factory check at a final-gate read. Both rows can
// share the very same operation-naming convention; only the source column
// tells them apart, and BugFailureReasons (a real final-gate-adjacent read)
// must never surface a fixer row's outcome as if it were a factory one.
func TestBugSelfCheck_SourceNeverSatisfiesAFinalGate(t *testing.T) {
	t.Parallel()
	db, session, request := bugSelfCheckFixture(t, 441)
	recipe := bugFixtureCheckRecipe()

	regressionBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "regression")
	start, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugSelfCheckPhase(t.Context(), session.ID, start.Operation, "failed", "self-check caught it early", nil); err != nil {
		t.Fatal(err)
	}

	phase, err := db.GetBugCheckPhase(t.Context(), session.ID, start.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if phase.Source != "fixer" {
		t.Fatalf("source = %q, want fixer", phase.Source)
	}

	// BugFailureReasons only ever reads source='factory' rows from bug_checks;
	// a fixer's own self-check outcome must not appear in it.
	reasons, err := db.BugFailureReasons(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reasons {
		if r.Reference == start.Operation {
			t.Fatalf("a fixer self-check row satisfied the factory-only failure-reason read: %+v", r)
		}
	}

	// FinishBugCheckPhase, the factory's own settlement path, refuses a
	// fixer-owned operation outright rather than quietly promoting it.
	if err := db.FinishBugCheckPhase(t.Context(), session.ID, start.Operation, "passed", "", true); err == nil {
		t.Fatal("the factory settlement path accepted a fixer-owned operation")
	}
}

// TestStopBugSelfChecks_CancelsPendingWithoutRerunning is AC-011: a crash-left
// pending self-check run is cancelled on restart recovery, never rerun, and
// PendingBugSelfCheckRepairs finds exactly the repairs that still owe it.
func TestStopBugSelfChecks_CancelsPendingWithoutRerunning(t *testing.T) {
	t.Parallel()
	db, session, request := bugSelfCheckFixture(t, 442)
	recipe := bugFixtureCheckRecipe()
	regressionBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "regression")
	start, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
	if err != nil {
		t.Fatal(err)
	}

	pending, err := db.PendingBugSelfCheckRepairs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pending {
		if p.SessionID == session.ID && p.Repair == request {
			found = true
		}
	}
	if !found {
		t.Fatalf("pending repairs = %+v, want %d/%s", pending, session.ID, request)
	}

	stopped, err := db.StopBugSelfChecks(t.Context(), session.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if stopped != 1 {
		t.Fatalf("stopped = %d, want 1", stopped)
	}

	phase, err := db.GetBugCheckPhase(t.Context(), session.ID, start.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if phase.Outcome != "cancelled" {
		t.Fatalf("outcome = %q, want cancelled", phase.Outcome)
	}

	// Idempotent: nothing left pending, so a second sweep stops nothing.
	again, err := db.StopBugSelfChecks(t.Context(), session.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second stop = %d, want 0", again)
	}

	pending, err = db.PendingBugSelfCheckRepairs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pending {
		if p.SessionID == session.ID && p.Repair == request {
			t.Fatalf("repair %s still listed as pending after being stopped", request)
		}
	}

	// The cancelled run is never rerun: a fresh ordinal-0 start allocates run 2.
	fresh, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, recipe, 1)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Ordinal != 2 {
		t.Fatalf("fresh start ordinal = %d, want 2 (run 1 stays cancelled, not reused)", fresh.Ordinal)
	}
}

// TestStartBugSelfCheck_RequiresRepairOwnWorktreeGuard pins AC-007: a
// self-check only runs under its own repair's worktree guard, never a
// foreign one and never none at all.
func TestStartBugSelfCheck_RequiresRepairOwnWorktreeGuard(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 443)
	request := "repair-1"
	in := bugFixtureRepairRequest(session, revision, approval, request, "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}
	// Dispatch is never claimed: no worktree guard is held for this repair.
	regressionBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "regression")
	if _, err := db.StartBugSelfCheck(t.Context(), session.ID, request, 0, regressionBinding, bugFixtureCheckRecipe(), 1); err == nil {
		t.Fatal("self-check started without a dispatched repair or its worktree guard")
	}
}

package store

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

// bugFixtureCheckRecipe is a minimal recipe satisfying config.ParseBugCheckRecipe.
func bugFixtureCheckRecipe() []byte {
	return []byte(`version: 1
phases:
  regression:
    commands:
      - argv: ["go", "test", "./..."]
        cwd: "."
        timeout_seconds: 60
  focused:
    commands:
      - argv: ["go", "test", "./pkg/..."]
        cwd: "."
        timeout_seconds: 60
  full:
    commands:
      - argv: ["go", "build", "./..."]
        cwd: "."
        timeout_seconds: 60
`)
}

func bugFixtureCheckBinding(session int64, operation, generation, repair, phase string) BugCheckBinding {
	recipe := bugFixtureCheckRecipe()
	return BugCheckBinding{
		SessionID: session, Operation: operation, Generation: generation, Repair: repair, Phase: phase,
		SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40),
		TargetRepository: "acme/api", TargetBranch: "main", TargetCommit: strings.Repeat("c", 40),
		RecipeDigest: config.BugRecipeDigest(recipe), EnvironmentRevision: "env-1", PermissionRevision: "perm-1",
	}
}

func bugFixtureCompletePassingCommand(t *testing.T, db *DB, session int64, operation string, ordinal int) {
	t.Helper()
	claimed, err := db.ClaimBugCheckCommand(t.Context(), session, operation, ordinal)
	if err != nil {
		t.Fatalf("claim command %d: %v", ordinal, err)
	}
	if !claimed {
		t.Fatalf("command %d was not claimed", ordinal)
	}
	exit := 0
	obs := BugCheckObservation{
		ExecutableDigest: strings.Repeat("d", 64), Executable: "go", CWD: ".", Started: bugFixtureTime(0), Ended: bugFixtureTime(time.Second),
		Exit: &exit, Outcome: "passed", Output: "ok", CaptureComplete: true, CleanupConfirmed: true, ProcessIdentity: "pid-1",
	}
	if err := db.ObserveBugCheckCommand(t.Context(), session, operation, ordinal, true, obs); err != nil {
		t.Fatalf("observe command %d: %v", ordinal, err)
	}
}

// TestEnsureBugCheckPhase_ReplayRetainsEarlierEvidence pins that a repeated
// EnsureBugCheckPhase call for the same operation and binding is a no-op, and
// a genuinely different binding is a replay conflict.
func TestEnsureBugCheckPhase_ReplayRetainsEarlierEvidence(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 430)
	binding := bugFixtureCheckBinding(session.ID, "check-1", "1", "repair-1", "regression")
	recipe := bugFixtureCheckRecipe()

	first, err := db.EnsureBugCheckPhase(t.Context(), binding, recipe)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if first.Count != 1 || first.Outcome != "pending" || first.Source != "factory" {
		t.Fatalf("phase = %+v", first)
	}

	second, err := db.EnsureBugCheckPhase(t.Context(), binding, recipe)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.Count != first.Count || second.Outcome != first.Outcome {
		t.Fatalf("replay = %+v, want identical to %+v", second, first)
	}

	changed := binding
	changed.SourceCommit = strings.Repeat("f", 40)
	if _, err := db.EnsureBugCheckPhase(t.Context(), changed, recipe); err == nil {
		t.Fatal("a changed binding for the same operation was accepted as a replay")
	}
}

// TestClaimBugCheckCommand_RequiresExclusiveGuardAndSequentialOrder pins that
// a command cannot be claimed without the phase's own worktree guard, and that
// commands must be claimed strictly in order after the previous one passed
// with confirmed cleanup.
func TestClaimBugCheckCommand_RequiresExclusiveGuardAndSequentialOrder(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 431)
	operation := "check-1"
	binding := bugFixtureCheckBinding(session.ID, operation, "1", "repair-1", "regression")
	if _, err := db.EnsureBugCheckPhase(t.Context(), binding, bugFixtureCheckRecipe()); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ClaimBugCheckCommand(t.Context(), session.ID, operation, 0); err == nil {
		t.Fatal("claimed a command without the phase's own worktree guard")
	}

	claimed, err := db.ClaimBugWork(t.Context(), session.ID, operation, "checks")
	if err != nil || !claimed {
		t.Fatalf("claim work: claimed=%v err=%v", claimed, err)
	}

	// The recipe's regression phase has exactly one command (ordinal 0); a
	// second ordinal cannot be claimed before it exists.
	if ok, err := db.ClaimBugCheckCommand(t.Context(), session.ID, operation, 1); ok {
		t.Fatalf("claimed ordinal 1 before ordinal 0 existed: ok=%v err=%v", ok, err)
	}

	bugFixtureCompletePassingCommand(t, db, session.ID, operation, 0)

	phase, err := db.GetBugCheckPhase(t.Context(), session.ID, operation)
	if err != nil {
		t.Fatal(err)
	}
	if len(phase.Commands) != 1 || phase.Commands[0].State != "completed" || phase.Commands[0].Observation.Outcome != "passed" {
		t.Fatalf("phase commands = %+v", phase.Commands)
	}

	if err := db.FinishBugCheckPhase(t.Context(), session.ID, operation, "passed", "", true); err != nil {
		t.Fatalf("finish: %v", err)
	}
	finished, err := db.GetBugCheckPhase(t.Context(), session.ID, operation)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Outcome != "passed" {
		t.Fatalf("outcome = %q, want passed", finished.Outcome)
	}

	// A terminal phase outcome cannot be rewritten to a different outcome.
	if err := db.FinishBugCheckPhase(t.Context(), session.ID, operation, "failed", "changed my mind", true); err == nil {
		t.Fatal("a passed phase accepted a different terminal outcome")
	}
}

// TestFinishBugCheckPhase_RefusesPassingWithoutCompleteEvidence pins that a
// phase cannot be marked passed while cancelled, unreliable, missing cleanup
// confirmation, or with a command that never completed.
func TestFinishBugCheckPhase_RefusesPassingWithoutCompleteEvidence(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 432)
	operation := "check-1"
	binding := bugFixtureCheckBinding(session.ID, operation, "1", "repair-1", "regression")
	if _, err := db.EnsureBugCheckPhase(t.Context(), binding, bugFixtureCheckRecipe()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimBugWork(t.Context(), session.ID, operation, "checks"); err != nil {
		t.Fatal(err)
	}

	if err := db.FinishBugCheckPhase(t.Context(), session.ID, operation, "passed", "", true); err == nil {
		t.Fatal("a phase with no completed command was marked passed")
	}

	if err := db.CancelBugCheckPhase(t.Context(), session.ID, operation); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimBugCheckCommand(t.Context(), session.ID, operation, 0); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.GetBugCheckPhase(t.Context(), session.ID, operation)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed.Cancelled {
		t.Fatal("cancellation did not persist")
	}
}

// TestBugFailureReasons_ReadsFactoryChecksAndFailureEvents is AC-008: the
// fixer's "why did it fail before" read joins non-passing factory bug_checks
// phases with retained bug_events failure records, and never surfaces a
// fixer-owned self-check row.
func TestBugFailureReasons_ReadsFactoryChecksAndFailureEvents(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 433)
	operation := "check-1"
	binding := bugFixtureCheckBinding(session.ID, operation, "1", "repair-1", "regression")
	if _, err := db.EnsureBugCheckPhase(t.Context(), binding, bugFixtureCheckRecipe()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimBugWork(t.Context(), session.ID, operation, "checks"); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimBugCheckCommand(t.Context(), session.ID, operation, 0)
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if err := db.ObserveBugCheckCommand(t.Context(), session.ID, operation, 0, true, BugCheckObservation{
		Outcome: "failed", Reason: "assertion mismatch", Output: "FAIL", CleanupConfirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugCheckPhase(t.Context(), session.ID, operation, "failed", "regression command failed", true); err != nil {
		t.Fatal(err)
	}

	if err := db.RecordBugFailure(t.Context(), session.ID, bugCurrentRevision(t, db, session.ID), "setup-step-1", "worktree preparation failed", []string{"evidence-a"}); err != nil {
		t.Fatal(err)
	}

	reasons, err := db.BugFailureReasons(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawCheck, sawFailureEvent bool
	for _, r := range reasons {
		if r.Reference == operation {
			sawCheck = true
			if r.Outcome != "failed" || r.Reason != "regression command failed" {
				t.Fatalf("check reason = %+v", r)
			}
		}
		if r.Reference == "setup-step-1" {
			sawFailureEvent = true
			if r.Reason != "worktree preparation failed" || len(r.Evidence) != 1 || r.Evidence[0] != "evidence-a" {
				t.Fatalf("failure event reason = %+v", r)
			}
		}
	}
	if !sawCheck || !sawFailureEvent {
		t.Fatalf("reasons = %+v, want both the failed check and the failure event", reasons)
	}
}

// TestRecordBugFailure_ReplayIsIdempotentAndRejectsChange pins the idempotent
// replay guarantee bug_events append relies on throughout the package.
func TestRecordBugFailure_ReplayIsIdempotentAndRejectsChange(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 434)

	rev := bugCurrentRevision(t, db, session.ID)
	if err := db.RecordBugFailure(t.Context(), session.ID, rev, "step-1", "reason A", nil); err != nil {
		t.Fatal(err)
	}
	revAfter := bugCurrentRevision(t, db, session.ID)
	if revAfter == rev {
		t.Fatal("recording a failure did not bump the optimistic revision")
	}

	// An exact replay (even with a stale expected revision) is a no-op.
	if err := db.RecordBugFailure(t.Context(), session.ID, rev, "step-1", "reason A", nil); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if bugCurrentRevision(t, db, session.ID) != revAfter {
		t.Fatal("replaying an identical failure bumped the revision again")
	}

	// A changed reason for the same reference is refused, not silently applied.
	if err := db.RecordBugFailure(t.Context(), session.ID, revAfter, "step-1", "reason B", nil); err == nil {
		t.Fatal("a changed failure reason for the same reference was accepted")
	}
}

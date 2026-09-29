package store

// Integration coverage for story-003 (issue #427): a real end-to-end repair
// dispatch, with a fixer self-check run (regression then focused phase)
// recorded under the repair's own worktree guard, never spending repair
// budget of its own, and whose summary is bound into the same delivery
// review digest a recorded fact is (so a new self-check run makes a
// published review stale, exactly like AC-009 describes).
//
// The real sandboxed process execution this feature also specifies
// (bugchecks.Isolation, regression then focused, against a minimal fixture
// repository) is covered separately in
// internal/bugchecks/self_check_sandbox_integration_test.go, which requires
// LF_TEST_CODEX (a real Codex CLI) and is skipped when that is unset, exactly
// as internal/bugchecks/sandbox_integration_test.go already does for its own
// scenarios — this environment does not have it.

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

const bugSelfCheckRecipeYAML = `
version: 1
phases:
  regression:
    commands:
      - argv: ["/bin/sh", "-c", "echo regression-ok"]
        cwd: "."
        timeout_seconds: 30
  focused:
    commands:
      - argv: ["/bin/sh", "-c", "echo focused-ok"]
        cwd: "."
        timeout_seconds: 30
  full:
    commands:
      - argv: ["/bin/sh", "-c", "echo full-ok"]
        cwd: "."
        timeout_seconds: 30
`

// TestBugSelfChecks_RepairDispatchRunsRegressionThenFocusedWithoutSpendingBudget
// drives one real dispatched repair attempt, then a fixer self-check run
// through both of its phases (regression, then focused once regression
// passed), and confirms: the run requires the repair's own worktree guard
// (AC-007), it never touches the repair's own state or the lifetime repair
// count (AC-005), and its passing summary changes the delivery review digest
// the same way a recorded fact does (AC-009) — so a review published before
// the self-check ran is stale against one computed after.
func TestBugSelfChecks_RepairDispatchRunsRegressionThenFocusedWithoutSpendingBudget(t *testing.T) {
	t.Parallel()
	f := newBugFixture(t, 4003)
	ctx := f.t.Context()

	instance := f.newInstance("record")
	f.ensureLifecycle(instance)
	approved := f.buildApprovedAnalysis(instance)

	const request = "repair-1"
	sourceCommit, sourceTree := bugSourceSHA(1), bugSourceSHA(2)

	reserveAttempt := f.startAttempt(instance, instance.CurrentNode)
	if _, err := f.db.ReserveBugRepair(ctx, BugRepairRequest{
		SessionID: f.session.ID, Request: request, AnalysisRevision: approved.Revision, AnalysisDigest: approved.Digest,
		ApprovalReview: approved.Review, Cause: "initial", Evidence: "initial repair attempt",
		SourceCommit: sourceCommit, SourceTree: sourceTree, GraphAttemptID: reserveAttempt.ID,
	}, f.revision()); err != nil {
		t.Fatalf("reserve repair: %v", err)
	}
	if err := f.db.FinishGraphNodeAttempt(ctx, reserveAttempt.ID, GraphSucceeded, "success", f.now()); err != nil {
		t.Fatalf("finish reservation attempt: %v", err)
	}

	segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, FirstSegmentOrdinal, "bug-fixer", AgentClaude)
	if err != nil {
		t.Fatalf("segment: %v", err)
	}
	run, err := f.db.AllocateRun(ctx, AllocateRunParams{
		SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude,
		TriggerCommentID: 2, StartedAt: f.now(), LogPath: func(int) string { return "/logs/bug-fixer.jsonl" },
	})
	if err != nil {
		t.Fatalf("allocate fixer run: %v", err)
	}
	dispatchAttempt := f.startAttemptForRun(instance, instance.CurrentNode, run.ID)
	claimed, err := f.db.ClaimBugRepairDispatch(ctx, f.session.ID, request, sourceCommit, sourceTree, dispatchAttempt.ID, run.ID)
	if err != nil {
		t.Fatalf("claim repair dispatch: %v", err)
	}
	if !claimed {
		t.Fatal("repair dispatch was not claimed")
	}

	recipe := []byte(bugSelfCheckRecipeYAML)
	parsed, err := config.ParseBugCheckRecipe(recipe)
	if err != nil {
		t.Fatalf("parse recipe: %v", err)
	}
	recipeDigest := config.BugRecipeDigest(recipe)

	baseBinding := BugCheckBinding{
		SessionID: f.session.ID, Repair: request, SourceCommit: sourceCommit, SourceTree: sourceTree,
		TargetRepository: "acme/api", TargetBranch: "main", TargetCommit: sourceCommit,
		RecipeDigest: recipeDigest, EnvironmentRevision: "env-1", PermissionRevision: "perm-1",
	}

	beforeRepairs := bugRepairCount(t, f)

	// Regression phase.
	regressionBinding := baseBinding
	regressionBinding.Phase = "regression"
	started, err := f.db.StartBugSelfCheck(ctx, f.session.ID, request, 0, regressionBinding, recipe, len(parsed.Phases.Regression.Commands))
	if err != nil {
		t.Fatalf("start regression self-check: %v", err)
	}
	if started.Status != "started" || started.Phase != "regression" || started.Ordinal != 1 {
		t.Fatalf("regression self-check start = %+v, want a fresh started regression run", started)
	}
	if err := f.db.FinishBugSelfCheckPhase(ctx, f.session.ID, started.Operation, "passed", "", []BugCheckCommandRecord{
		{Ordinal: 0, State: "completed", Observation: BugCheckObservation{Outcome: "passed", CaptureComplete: true, CleanupConfirmed: true}},
	}); err != nil {
		t.Fatalf("finish regression self-check: %v", err)
	}

	// Focused phase, only reachable once regression passed.
	focusedBinding := baseBinding
	focusedBinding.Phase = "focused"
	focusedStarted, err := f.db.StartBugSelfCheck(ctx, f.session.ID, request, started.Ordinal, focusedBinding, recipe, len(parsed.Phases.Focused.Commands))
	if err != nil {
		t.Fatalf("start focused self-check: %v", err)
	}
	if focusedStarted.Status != "started" || focusedStarted.Phase != "focused" {
		t.Fatalf("focused self-check start = %+v, want a started focused run", focusedStarted)
	}
	if err := f.db.FinishBugSelfCheckPhase(ctx, f.session.ID, focusedStarted.Operation, "passed", "", []BugCheckCommandRecord{
		{Ordinal: 0, State: "completed", Observation: BugCheckObservation{Outcome: "passed", CaptureComplete: true, CleanupConfirmed: true}},
	}); err != nil {
		t.Fatalf("finish focused self-check: %v", err)
	}

	runs, err := f.db.BugSelfChecks(ctx, f.session.ID, request)
	if err != nil {
		t.Fatalf("bug self checks: %v", err)
	}
	if len(runs) != 1 || len(runs[0].Phases) != 2 || !runs[0].Done() {
		t.Fatalf("self-check runs = %+v, want one complete run with both phases", runs)
	}
	for _, p := range runs[0].Phases {
		if p.Outcome != "passed" {
			t.Fatalf("phase %q outcome = %q, want passed", p.Binding.Phase, p.Outcome)
		}
		if p.Source != "fixer" {
			t.Fatalf("self-check phase source = %q, want fixer (AC-002/AC-005 never touch the factory's own final-gate evidence)", p.Source)
		}
	}

	// AC-005: the self-check run never touched the lifetime repair count or the
	// repair's own state.
	if got := bugRepairCount(t, f); got != beforeRepairs {
		t.Fatalf("self-check run changed the repair row count: before=%d after=%d", beforeRepairs, got)
	}
	var repairState string
	if err := f.db.sql.QueryRowContext(ctx, `SELECT state FROM bug_repairs WHERE session_id=? AND request=?`, f.session.ID, request).Scan(&repairState); err != nil {
		t.Fatalf("read repair state: %v", err)
	}
	if repairState != "dispatched" {
		t.Fatalf("repair state = %q after a self-check run, want it left exactly as the self-check found it (dispatched)", repairState)
	}

	// AC-009: the self-check summary is bound into the delivery review digest
	// the same way a fact is — a review computed without it disagrees with one
	// computed after it exists.
	summary := SummarizeBugSelfChecks(runs)
	evidence, pr := BugDeliveryEvidence{}, BugPullRequest{}
	digestWithout, err := BugDeliveryReviewDigest(evidence, pr, nil, nil)
	if err != nil {
		t.Fatalf("digest without self-checks: %v", err)
	}
	digestWith, err := BugDeliveryReviewDigest(evidence, pr, nil, summary)
	if err != nil {
		t.Fatalf("digest with self-checks: %v", err)
	}
	if digestWithout == digestWith {
		t.Fatal("the self-check evidence did not change the delivery review digest: a published review would not go stale once it exists")
	}
}

// TestBugSelfChecks_RequiresTheRepairsOwnWorktreeGuard is AC-007: a self-check
// cannot start without the exact repair's own guard held, so it can never run
// concurrently with anything else touching the worktree.
func TestBugSelfChecks_RequiresTheRepairsOwnWorktreeGuard(t *testing.T) {
	t.Parallel()
	f := newBugFixture(t, 4004)
	ctx := f.t.Context()

	instance := f.newInstance("record")
	f.ensureLifecycle(instance)
	approved := f.buildApprovedAnalysis(instance)

	const request = "repair-1"
	sourceCommit, sourceTree := bugSourceSHA(3), bugSourceSHA(4)
	reserveAttempt := f.startAttempt(instance, instance.CurrentNode)
	if _, err := f.db.ReserveBugRepair(ctx, BugRepairRequest{
		SessionID: f.session.ID, Request: request, AnalysisRevision: approved.Revision, AnalysisDigest: approved.Digest,
		ApprovalReview: approved.Review, Cause: "initial", Evidence: "initial repair attempt",
		SourceCommit: sourceCommit, SourceTree: sourceTree, GraphAttemptID: reserveAttempt.ID,
	}, f.revision()); err != nil {
		t.Fatalf("reserve repair: %v", err)
	}
	// The repair is reserved but never dispatched: no worktree guard is held.

	recipe := []byte(bugSelfCheckRecipeYAML)
	binding := BugCheckBinding{
		SessionID: f.session.ID, Repair: request, Phase: "regression", SourceCommit: sourceCommit, SourceTree: sourceTree,
		TargetRepository: "acme/api", TargetBranch: "main", TargetCommit: sourceCommit,
		RecipeDigest: config.BugRecipeDigest(recipe), EnvironmentRevision: "env-1", PermissionRevision: "perm-1",
	}
	if _, err := f.db.StartBugSelfCheck(ctx, f.session.ID, request, 0, binding, recipe, 1); err == nil {
		t.Fatal("started a self-check without the repair's own dispatched worktree guard")
	}
}

func bugRepairCount(t *testing.T, f *bugFixture) int {
	t.Helper()
	var count int
	if err := f.db.sql.QueryRowContext(f.t.Context(), `SELECT count(*) FROM bug_repairs WHERE session_id=?`, f.session.ID).Scan(&count); err != nil {
		t.Fatalf("count bug_repairs: %v", err)
	}
	return count
}

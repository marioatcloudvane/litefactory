package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/report"
)

// seedTaskWorktree creates the exact branch and worktree [prepare]'s own
// ensureWorktree step would create on this task's first run — cut from
// origin/<base> at f.storeRepo.BaseBranch, at f.worktreeDir() — and returns
// the worktree directory.
//
// It has to run before [fixture.execute]: the worktree does not exist until
// prepareClone creates it, and on a first run the branch starts at the tip of
// the base, so there is nothing yet to conflict with. Seeding the branch here
// and diverging it afterwards (with [conflictBaseAndBranch]) is what lets a
// test put a real conflict in front of prepareClone's own ensureWorktree step,
// which then reuses this branch and this worktree exactly as
// [git.EnsureWorktree]'s reuse-first design promises.
func seedTaskWorktree(t *testing.T, f *fixture) string {
	t.Helper()

	dir := f.worktreeDir()
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatalf("git.Resolve: %v", err)
	}
	clone, err := binary.Open(t.Context(), f.repo.Dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", f.repo.Dir, err)
	}
	if _, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
		Dir:        dir,
		Branch:     testBranch,
		BaseBranch: gittest.DefaultBranch,
	}); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	return dir
}

func TestExecuteContinuesOnConflictingBase(t *testing.T) {
	t.Parallel()
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("fixture stops before save")
	})
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)
	var result Result
	gittest.AssertUnchanged(t, dir, func() { result = f.execute(f.request()) })
	if runner.callCount() != 1 || result.Run.ID == 0 || result.Sync == nil || result.Sync.Outcome != report.SyncBlocked {
		t.Fatalf("deferred conflict did not reach work: %+v", result)
	}
}

// TestExecuteDoesNotRefuseASyncWhenBlockRunOnConflictIsFalse is the other half
// of the [Options.BlockRunOnConflict] gate this story adds: the same
// conflicting base does not stop the run when the operator has configured
// disclosure instead of blocking. It is the run itself that is out of scope
// here (US-2's own decision tree, and US-3's disclosure) — this test only
// pins that [DispositionSyncRefused] is not reached when the flag is false.
func TestExecuteDoesNotRefuseASyncWhenBlockRunOnConflictIsFalse(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = false

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)

	result := f.execute(f.request())

	if result.Disposition == DispositionSyncRefused {
		t.Fatal("disposition = DispositionSyncRefused though BlockRunOnConflict is false")
	}
	if runner.callCount() != 1 {
		t.Errorf("claude ran %d times, want 1: a disclosed-not-blocked conflict must not stop the run", runner.callCount())
	}
}

// TestReporterPostsTheSyncSkipReason is the disclosure half of US-3 exercised
// end to end, at the level a person actually reads: Execute through
// ReportTurn through the real internal/report renderer to the posted comment
// body. It is deliberately not a renderer test — internal/report's own golden
// files already prove a report.Sync renders correctly. What it proves is the
// seam between them, which is where AC-3.2 was lost: syncBase recorded the
// skip and prepareClone persisted it, but Result carried no sync outcome and
// the report.Run literal never set Sync, so every skip rendered as silence.
//
// Skipping is the case worth pinning: a skipped run goes ahead against a
// branch that was left behind, and a skip nobody is told about is exactly the
// quiet staleness the whole feature exists to prevent.
func TestReporterPostsTheSyncSkipReason(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	// Explicitly disabled synchronization remains observable when behind.
	f.opts.AutoSyncBase = false

	seedTaskWorktree(t, f)
	advanceSyncBase(t, f.repo, 2)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	if result.Disposition != DispositionRan {
		t.Fatalf("disposition = %s, want %s: a skip lets the run proceed\nlogs:\n%s",
			result.Disposition, DispositionRan, f.logs)
	}
	if result.Sync == nil {
		t.Fatalf("Result.Sync is nil though sync was skipped: the outcome never left prepare\nlogs:\n%s", f.logs)
	}
	if result.Sync.Outcome != report.SyncSkipped {
		t.Errorf("Result.Sync.Outcome = %q, want %q", result.Sync.Outcome, report.SyncSkipped)
	}
	if result.Sync.Reason == "" {
		t.Error("Result.Sync.Reason is empty: a skip has to say why (AC-3.2)")
	}

	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}
	if posted.Skipped {
		t.Fatal("Skipped = true, want the run report posted")
	}

	body := forge.only(t).body
	switch {
	case !strings.Contains(body, "Preparation caveat"):
		t.Errorf("the posted report carries no sync section though sync was skipped (AC-3.2):\n%s", body)
	case !strings.Contains(body, "base synchronization"):
		t.Errorf("the sync section does not name the skip:\n%s", body)
	case !strings.Contains(body, "auto-sync is turned off"):
		t.Errorf("the sync section does not say why it skipped (AC-3.2):\n%s", body)
	}
}

func TestExecuteContinuesBeyondAutomaticMergeCap(t *testing.T) {
	t.Parallel()
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("fixture stops before save")
	})
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.AutoSyncMaxBehind = 1
	dir := seedTaskWorktree(t, f)
	advanceSyncBase(t, f.repo, 2)
	var result Result
	gittest.AssertUnchanged(t, dir, func() { result = f.execute(f.request()) })
	if runner.callCount() != 1 || result.Sync == nil || result.Sync.Outcome != report.SyncSkipped || !strings.Contains(result.Sync.Reason, "limit of 1") {
		t.Fatalf("over-cap preparation did not continue: %+v", result)
	}
}

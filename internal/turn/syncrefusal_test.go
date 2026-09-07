package turn

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
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

// TestExecuteRefusesASyncOnAConflictingBase is AC-4.1 exercised as one
// coherent scenario, at the level Execute actually runs at: given a same-line
// conflict between the task branch and the base branch, the agent never
// starts, the turn's disposition is [DispositionSyncRefused] (which
// [Reporter.ReportTurn] answers with [report.ReplyRefusesSync] —
// see TestReporterPostsReplyRefusesSyncWithConflictingPaths for the rendered
// half of that), the conflicting paths are carried on [Result.SyncConflicts],
// and the branch SHA is unchanged from before the run — proven with
// [gittest.AssertUnchanged] rather than assumed from "MergePreview never
// mutates anything", following this codebase's own S-5 discipline.
func TestExecuteRefusesASyncOnAConflictingBase(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran though the base branch could not be merged in cleanly")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")

	var result Result
	gittest.AssertUnchanged(t, dir, func() {
		result = f.execute(f.request())
	})

	switch {
	case result.Disposition != DispositionSyncRefused:
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionSyncRefused, f.logs)
	case !result.Reportable():
		t.Error("Reportable() = false: a sync refusal still owes the issue a comment (AC-4.1)")
	case !result.ConsumesComment():
		t.Error("ConsumesComment() = false: a sync refusal consumes the comment it answers, the same as an agent-switch refusal")
	case len(result.SyncConflicts) == 0:
		t.Error("SyncConflicts is empty, want the conflicting path(s) MergePreview found")
	case !slices.Contains(result.SyncConflicts, "README.md"):
		t.Errorf("SyncConflicts = %v, want it to contain README.md", result.SyncConflicts)
	}
	if runner.callCount() != 0 {
		t.Errorf("claude ran %d times, want 0: the agent must never start on a sync refusal", runner.callCount())
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs on a sync refusal, want 0", len(runs))
	}
	if state := f.reloadSession().State; state != store.SessionIdle {
		t.Errorf("session state after a sync refusal = %q, want %q (the refusal touched no state)",
			state, store.SessionIdle)
	}
	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after a sync refusal = %q, want it released", owner)
	}

	after := f.repo.GitIn(dir, "rev-parse", "HEAD")
	if after != before {
		t.Errorf("branch HEAD moved from %s to %s though the run was refused before starting", before, after)
	}

	// No run-artifact directory: allocateRun is never reached on this path, so
	// there is no ordinal and nothing under $LF_HOME/logs/runs for this task.
	runsDir, err := naming.RunsDir(f.paths.Home(), f.ref())
	if err != nil {
		t.Fatalf("naming.RunsDir: %v", err)
	}
	if _, statErr := os.Stat(runsDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("run-artifact directory %s exists though no run was allocated for a sync refusal", runsDir)
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

// TestReporterPostsReplyRefusesSyncWithConflictingPaths closes CR-A-002: an
// end-to-end test of the full ReplyRefusesSync flow, from Execute through
// reportSyncRefusal to ReportReply to the real internal/report renderer to
// the posted comment body — not merely the three individual behaviors
// (agent never starts, no run-artifact directory, refusal comment posted)
// each covered in isolation.
func TestReporterPostsReplyRefusesSyncWithConflictingPaths(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran though the base branch could not be merged in cleanly")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	if result.Disposition != DispositionSyncRefused {
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionSyncRefused, f.logs)
	}

	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}
	if posted.Skipped {
		t.Fatal("Skipped = true, want a sync refusal to post a comment (AC-4.1)")
	}

	post := forge.only(t)
	switch {
	case !strings.HasPrefix(post.body, report.ReplyMarker(f.ref())):
		t.Errorf("body does not begin with the reply marker:\n%s", post.body)
	case strings.Contains(post.body, "run="):
		t.Errorf("a sync refusal carries a run attribute though no run happened:\n%s", post.body)
	case !strings.Contains(post.body, "could not be merged with the base branch cleanly"):
		t.Errorf("body does not name the merge refusal:\n%s", post.body)
	case !strings.Contains(post.body, "Nothing was run"):
		t.Errorf("body does not carry the refusal sentence naming that nothing ran:\n%s", post.body)
	case !strings.Contains(post.body, "Conflicting path(s) (1)"):
		t.Errorf("body does not list the conflicting path count:\n%s", post.body)
	case !strings.Contains(post.body, "README.md"):
		t.Errorf("body does not name the conflicting path README.md:\n%s", post.body)
	}
	if posted.Run.ID != 0 {
		t.Errorf("Posted.Run.ID = %d, want 0: a sync refusal closes out no run", posted.Run.ID)
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs on a sync refusal, want 0", len(runs))
	}
	if wm, exists, err := f.db.GetWatermark(t.Context(), f.session.ID); err != nil {
		t.Fatalf("read watermark: %v", err)
	} else if !exists || wm.LastCommentID != testComment {
		t.Errorf("watermark = %+v exists=%t, want it advanced to %d: the refusal consumed its comment",
			wm, exists, testComment)
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
	f.opts.AutoSyncBase = true
	// Two commits behind against a cap of one: over the cap by construction,
	// without building a hundred commits to get there.
	f.opts.AutoSyncMaxBehind = 1

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
	case !strings.Contains(body, "Base branch sync"):
		t.Errorf("the posted report carries no sync section though sync was skipped (AC-3.2):\n%s", body)
	case !strings.Contains(body, "skipped syncing this branch"):
		t.Errorf("the sync section does not name the skip:\n%s", body)
	case !strings.Contains(body, "more than the configured cap"):
		t.Errorf("the sync section does not say why it skipped (AC-3.2):\n%s", body)
	}
}

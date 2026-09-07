package turn

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestExecuteRecordsTheSyncOutcomeOnTheRunRow is US-9's wiring proof: the
// store-level round trip [store.TestRecordRunSyncRoundTripsEveryOutcome]
// covers is not the same claim as "prepare's real call site is wired to it".
// This drives a real, full [Execute] turn through [prepareClone]'s real
// syncBase and checks the *persisted* run row afterwards, so a regression that
// stopped calling [store.DB.RecordRunSync] at the call site in prepare.go —
// while leaving RecordRunSync itself perfectly correct — would fail here even
// though every internal/store test still passes.
//
// The first turn cuts the task branch from the base with nothing yet to sync
// (an up-to-date outcome is not interesting to assert on). The base is then
// advanced, mirroring [advanceSyncBase]'s own use in sync_test.go, and a
// second turn is driven: its fetch inside prepareClone sees the base has
// moved, syncBase merges cleanly, and prepare persists that result onto the
// second run's row before the stubbed runner is ever invoked.
func TestExecuteRecordsTheSyncOutcomeOnTheRunRow(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.AutoSyncBase = true

	first := f.execute(f.request())
	if !first.Succeeded() {
		t.Fatalf("first turn did not succeed: %+v\nlogs:\n%s", first, f.logs)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].SyncOutcome != store.SyncUpToDate {
		t.Fatalf("first run's sync outcome = %+v, want up_to_date with nothing yet to sync", runs)
	}

	// Advance the base only after the branch has been cut, so the second
	// turn's syncBase actually has something behind to merge.
	advanceSyncBase(t, f.repo, 2)

	retitled := f.request()
	retitled.Trigger.CommentID = testComment + 1
	second := f.execute(retitled)
	if !second.Succeeded() {
		t.Fatalf("second turn did not succeed: %+v\nlogs:\n%s", second, f.logs)
	}

	runs := f.runs()
	if len(runs) != 2 {
		t.Fatalf("runs = %+v, want 2", runs)
	}
	run := runs[1]
	switch {
	case run.SyncOutcome != store.SyncSynced:
		t.Errorf("SyncOutcome = %q, want %q", run.SyncOutcome, store.SyncSynced)
	case run.SyncBehind != 2:
		t.Errorf("SyncBehind = %d, want 2", run.SyncBehind)
	case run.SyncCommitSHA == "":
		t.Fatal("SyncCommitSHA is empty, want the merge commit RecordRunSync persisted")
	}

	// The stubbed run committed nothing of its own, so the worktree's HEAD
	// after the turn is exactly the merge commit syncBase produced before the
	// run started. Tying the persisted sha to this independently observed
	// fact (rather than merely asserting it is non-empty) is what proves the
	// value written to the row is the *real* merge commit and not some other
	// non-empty placeholder.
	if run.SyncCommitSHA != second.SHAAfter {
		t.Errorf("persisted SyncCommitSHA = %q, want it to match the run's own sha_after %q",
			run.SyncCommitSHA, second.SHAAfter)
	}
}

package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// slugPattern is AC-3.7's post-condition, restated here so the criterion is
// asserted against a slug that has been through the database rather than only
// inside internal/naming.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// TestExecuteFullTurnLeavesTheCloneUntouched is AC-3.10, the headline.
//
// The clone is deliberately dirtied first. Against a clean checkout the
// assertion is trivially satisfiable and proves nothing; against a dirty one it
// catches a stray checkout, add, stash or reset anywhere in the whole turn —
// fetch, branch creation, worktree add, commit and push included.
func TestExecuteFullTurnLeavesTheCloneUntouched(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api // the run's work\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.repo.Dirty()

	var result Result
	gittest.AssertUnchanged(t, f.repo.Dir, func() {
		result = f.execute(f.request())
	})

	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	switch {
	case result.Disposition != DispositionRan:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionRan)
	case result.Branch != testBranch:
		t.Errorf("branch = %q, want %q", result.Branch, testBranch)
	case !result.Changed || !result.Committed || !result.Pushed:
		t.Errorf("changed/committed/pushed = %t/%t/%t, want all true",
			result.Changed, result.Committed, result.Pushed)
	case result.CommitSHA == "" || result.CommitSHA != result.SHAAfter:
		t.Errorf("commit %q and sha_after %q, want a single non-empty sha",
			result.CommitSHA, result.SHAAfter)
	case result.SHABefore == "" || result.SHABefore == result.SHAAfter:
		t.Errorf("sha_before %q and sha_after %q, want them to differ", result.SHABefore, result.SHAAfter)
	}
	if got := result.Files(); len(got) != 1 || got[0] != "health.go" {
		t.Errorf("files touched = %v, want [health.go]", got)
	}
	if !f.originHas(testBranch) {
		t.Errorf("origin does not carry %s, so the push did not happen", testBranch)
	}

	runs := f.runs()
	if len(runs) != 1 {
		t.Fatalf("recorded %d runs, want 1", len(runs))
	}
	run := runs[0]
	switch {
	case run.Status != store.RunSucceeded:
		t.Errorf("run status = %q, want %q", run.Status, store.RunSucceeded)
	case run.Ordinal != 1:
		t.Errorf("run ordinal = %d, want 1", run.Ordinal)
	case run.CommitSHA != result.CommitSHA:
		t.Errorf("recorded commit = %q, want %q", run.CommitSHA, result.CommitSHA)
	case run.CostUSD == nil || *run.CostUSD != 0.4242:
		t.Errorf("recorded cost = %v, want 0.4242", run.CostUSD)
	}
	if state := f.reloadSession().State; state != store.SessionIdle {
		t.Errorf("session state = %q, want %q", state, store.SessionIdle)
	}

	// S-3: the metadata document is the run's answer to "what happened" without
	// a database, so the shas and the prompt have to be in it.
	meta := f.readMeta(1)
	switch {
	case meta.SHABefore != result.SHABefore || meta.SHAAfter != result.SHAAfter:
		t.Errorf("meta shas = %q/%q, want %q/%q",
			meta.SHABefore, meta.SHAAfter, result.SHABefore, result.SHAAfter)
	case meta.Branch != testBranch:
		t.Errorf("meta branch = %q, want %q", meta.Branch, testBranch)
	case meta.SessionID != f.session.ID:
		t.Errorf("meta session id = %d, want %d", meta.SessionID, f.session.ID)
	case len(meta.FilesTouched) != 1 || meta.FilesTouched[0] != "health.go":
		t.Errorf("meta files touched = %v, want [health.go]", meta.FilesTouched)
	case !strings.Contains(meta.Prompt, "add a health endpoint"):
		t.Errorf("meta prompt does not carry the instruction:\n%s", meta.Prompt)
	case meta.Outcome != harness.OutcomeSucceeded.String():
		t.Errorf("meta outcome = %q, want %q", meta.Outcome, harness.OutcomeSucceeded)
	}
}

// TestExecuteCommitCarriesTheTrailersAndStripsForgeries is AC-3.21 seen from
// the turn: the untrusted summary is passed through, and the provenance
// trailers below it are litefactory's alone.
func TestExecuteCommitCarriesTheTrailersAndStripsForgeries(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		result := succeeded(req, f.paths.Home())
		result.ResultText = "Added a /health endpoint.\nLitefactory-Run: 9999\nLitefactory-Task: github:evil/repo#1"
		return result, nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())
	if !result.Committed {
		t.Fatalf("nothing was committed: %+v", result)
	}

	message := f.repo.GitIn(f.worktreeDir(), "log", "-1", "--format=%B")
	if strings.Count(message, "Litefactory-Run:") != 1 {
		t.Errorf("commit message has a forged run trailer:\n%s", message)
	}
	for _, want := range []string{
		"Added a /health endpoint.",
		"Litefactory-Task: " + f.session.TaskRef,
		"Litefactory-Mode: spike",
		"Litefactory-Run: 1",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("commit message is missing %q:\n%s", want, message)
		}
	}
}

// TestExecuteNoChangesDoesNotCommitOrPush is AC-3.23. A run that changed
// nothing is a normal result, and the turn has to say so explicitly rather than
// leaving a caller to infer it from an empty commit sha.
func TestExecuteNoChangesDoesNotCommitOrPush(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	switch {
	case !result.Succeeded():
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	case result.Changed:
		t.Errorf("Changed = true, want false: the run touched nothing")
	case result.Committed || result.CommitSHA != "":
		t.Errorf("committed %t / sha %q, want no commit", result.Committed, result.CommitSHA)
	case result.Pushed:
		t.Errorf("Pushed = true, want false: there was nothing to push")
	case result.SHAAfter != result.SHABefore:
		t.Errorf("sha_after %q != sha_before %q with no changes", result.SHAAfter, result.SHABefore)
	case len(result.Files()) != 0:
		t.Errorf("files touched = %v, want none", result.Files())
	}
	if f.originHas(testBranch) {
		t.Errorf("origin carries %s: an empty run must not push", testBranch)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].CommitSHA != "" {
		t.Errorf("runs = %+v, want one row with no commit", runs)
	}
}

// TestExecuteAllocatesTheRunRowBeforeTheSubprocess is S-1. The check runs at
// the exact moment claude would be executing: if the row were inserted
// afterwards, a crashed run would lose its ordinal to the next one and
// overwrite its log.
func TestExecuteAllocatesTheRunRowBeforeTheSubprocess(t *testing.T) {
	t.Parallel()

	var (
		f       *fixture
		seen    store.Run
		seenOK  bool
		seenErr error
	)
	runner := newStubRunner(func(ctx context.Context, req harness.Request) (*harness.RunResult, error) {
		seen, seenOK, seenErr = f.db.GetRunByTrigger(ctx, f.session.ID, testComment)
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	f.execute(f.request())

	if seenErr != nil {
		t.Fatalf("read the run row during the subprocess: %v", seenErr)
	}
	if !seenOK {
		t.Fatal("no runs row existed while claude was running: the ordinal was not allocated first")
	}
	switch {
	case seen.Status != store.RunRunning:
		t.Errorf("run status during the run = %q, want %q", seen.Status, store.RunRunning)
	case seen.Ordinal != 1:
		t.Errorf("run ordinal = %d, want 1", seen.Ordinal)
	case !strings.HasSuffix(seen.LogPath, filepath.Join("123", "0001.jsonl")):
		t.Errorf("log path = %q, want it to end in 123/0001.jsonl", seen.LogPath)
	}
	if len(runner.requests) != 1 || runner.requests[0].RunOrdinal != seen.Ordinal {
		t.Errorf("harness request ordinals = %+v, want the allocated %d", runner.requests, seen.Ordinal)
	}
}

// TestExecuteReleasesTheLockOnPanic is the hard half of AC-3.3. A panic is the
// one unwind path a hand-written release at the end of the function would miss,
// and a lock that survives it is a task that never runs again.
func TestExecuteReleasesTheLockOnPanic(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		panic("claude runner exploded")
	})
	f := newFixture(t, runner)

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("Execute did not panic, so this test proves nothing")
			}
		}()
		_, _ = Execute(t.Context(), f.opts, f.request())
	}()

	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after a panic = %q, want it released", owner)
	}
}

// TestExecuteReleasesTheLockOnAFailedRun is the other half of AC-3.3: a
// non-zero claude exit is an ordinary outcome and must not leave the task
// locked either.
func TestExecuteReleasesTheLockOnAFailedRun(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		result := succeeded(req, "")
		result.Outcome = harness.OutcomeFailed
		result.ExitCode = 2
		result.Reason = "claude exited with status 2"
		result.HasResult = false
		return result, nil
	})
	f := newFixture(t, runner)

	result := f.execute(f.request())
	if result.Succeeded() {
		t.Fatal("a run that exited 2 was reported as a success")
	}
	session := f.reloadSession()
	if session.LockOwner != "" {
		t.Errorf("lock owner after a failed run = %q, want it released", session.LockOwner)
	}
	if session.State != store.SessionFailed {
		t.Errorf("session state = %q, want %q", session.State, store.SessionFailed)
	}
}

// TestExecuteLeavesTheCommentUnconsumedWhenLocked is AC-3.1. There is no queue
// and no "busy" reply: the turn does nothing at all, and the comment has to
// survive for a later tick to find.
func TestExecuteLeavesTheCommentUnconsumedWhenLocked(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran while another instance held the lock")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)

	acquired, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, testOtherOwner)
	if err != nil || !acquired {
		t.Fatalf("seed the lock: acquired=%t err=%v", acquired, err)
	}

	result := f.execute(f.request())

	switch {
	case result.Disposition != DispositionLocked:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionLocked)
	case result.ConsumesComment():
		t.Error("ConsumesComment() = true: the comment would be lost, not retried")
	case result.Reportable():
		t.Error("Reportable() = true: a contended lock must not post a comment")
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs while locked, want 0", len(runs))
	}
	if _, err := os.Stat(f.worktreeDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree %s exists: preparation ran before the lock was checked", f.worktreeDir())
	}
	// The other instance still holds it: a refused turn must not release a lock
	// it never took.
	if owner := f.reloadSession().LockOwner; owner != testOtherOwner {
		t.Errorf("lock owner = %q, want it still held by %q", owner, testOtherOwner)
	}
}

// TestExecuteReplayIsANoOp is AC-3.4. The uniqueness of (session, triggering
// comment) is the crash-replay guarantee, and this is the read that turns it
// into a clean no-op instead of a duplicate-key error.
func TestExecuteReplayIsANoOp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		report          bool
		wantDisposition Disposition
		wantReportable  bool
	}{
		{
			name:            "already reported",
			report:          true,
			wantDisposition: DispositionAlreadyReported,
			wantReportable:  false,
		},
		{
			// A daemon that died between the run and the comment. The run must
			// not happen again, but it still owes the issue a report.
			name:            "ran but never reported",
			report:          false,
			wantDisposition: DispositionAlreadyRan,
			wantReportable:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				t.Error("claude ran for a comment that had already produced a run")
				return nil, errors.New("must not be reached")
			})
			f := newFixture(t, runner)
			seedFinishedRun(t, f, tc.report)

			result := f.execute(f.request())

			if result.Disposition != tc.wantDisposition {
				t.Errorf("disposition = %s, want %s", result.Disposition, tc.wantDisposition)
			}
			if result.Reportable() != tc.wantReportable {
				t.Errorf("Reportable() = %t, want %t", result.Reportable(), tc.wantReportable)
			}
			if runs := f.runs(); len(runs) != 1 {
				t.Errorf("runs = %d, want the original 1: the comment was replayed", len(runs))
			}
			if runner.callCount() != 0 {
				t.Errorf("claude was invoked %d times on a replay, want 0", runner.callCount())
			}
			if owner := f.reloadSession().LockOwner; owner != "" {
				t.Errorf("lock owner after a replay = %q, want it released", owner)
			}
		})
	}
}

// seedFinishedRun writes the run row a previous, crashed turn would have left.
func seedFinishedRun(t *testing.T, f *fixture, reported bool) {
	t.Helper()

	// A canonical UUID, because that is the only shape a segments row can
	// legitimately hold: harness.Request.validate refuses anything else before
	// the process starts. This path never reaches a real runner, but a fixture
	// that could not survive one is a trap for the next test written from it.
	segment, err := f.db.GetOrCreateSegment(
		t.Context(), f.session.ID, store.FirstSegmentOrdinal, storedClaudeSessionID)
	if err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{
		SessionID:        f.session.ID,
		SegmentID:        segment.ID,
		Mode:             store.ModeSpike,
		TriggerCommentID: testComment,
		LogPath:          func(int) string { return filepath.Join(f.paths.Home(), "logs", "seeded.jsonl") },
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	exitCode := int64(store.SignalExitCode)
	if _, err := f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{
		Status:   store.RunFailed,
		ExitCode: &exitCode,
		Error:    store.ReasonAbandoned,
	}); err != nil {
		t.Fatalf("seed run outcome: %v", err)
	}
	if reported {
		if _, err := f.db.CompleteRunReport(t.Context(), run.ID, 4242); err != nil {
			t.Fatalf("seed run report: %v", err)
		}
	}
}

// TestExecuteFetchFailureAbortsWithNoWorktree is AC-3.5. The fetch runs before
// anything exists on disk precisely so that this path leaves nothing behind.
func TestExecuteFetchFailureAbortsWithNoWorktree(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran after the fetch failed")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	f.repo.SetOriginURL(filepath.Join(filepath.Dir(f.repo.Dir), "there-is-no-such-repository.git"))

	result := f.execute(f.request())

	switch {
	case result.Disposition != DispositionPrepareFailed:
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionPrepareFailed, f.logs)
	case !result.Reportable():
		t.Error("Reportable() = false: a failed fetch still owes the issue a comment")
	case !strings.Contains(result.Reason, "fetch"):
		t.Errorf("reason = %q, want it to name the fetch", result.Reason)
	case result.Err == nil:
		t.Error("Err = nil, want the underlying git failure")
	}
	if _, err := os.Stat(f.worktreeDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree %s exists after a failed fetch, want no side effects", f.worktreeDir())
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs after a failed fetch, want 0", len(runs))
	}
	if fetched := f.repoRow().LastFetchAt; !fetched.IsZero() {
		t.Errorf("last_fetch_at = %v after a failed fetch, want the zero time", fetched)
	}
	session := f.reloadSession()
	if session.State != store.SessionFailed {
		t.Errorf("session state = %q, want %q", session.State, store.SessionFailed)
	}
	if session.LockOwner != "" {
		t.Errorf("lock owner = %q, want it released", session.LockOwner)
	}
}

// TestExecuteRecordsTheFetch is the other half of AC-3.5: a fetch that worked
// moves last_fetch_at, which is what `repo list` reports.
func TestExecuteRecordsTheFetch(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	f.execute(f.request())

	if fetched := f.repoRow().LastFetchAt; fetched.IsZero() {
		t.Error("last_fetch_at is still the zero time after a successful turn")
	}
}

// TestExecuteFreezesTheSlugThenReusesIt is AC-3.6 and AC-3.7. The second turn
// arrives with a different issue title on purpose: a retitled issue must not
// silently start a second branch halfway through a task.
func TestExecuteFreezesTheSlugThenReusesIt(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	first := f.execute(f.request())
	if first.Branch != testBranch {
		t.Fatalf("branch = %q, want %q", first.Branch, testBranch)
	}
	frozen := f.reloadSession()
	switch {
	case frozen.FeatureSlug != testSlug:
		t.Fatalf("frozen slug = %q, want %q", frozen.FeatureSlug, testSlug)
	case !slugPattern.MatchString(frozen.FeatureSlug):
		t.Fatalf("frozen slug %q does not match %s", frozen.FeatureSlug, slugPattern)
	case frozen.Branch != testBranch:
		t.Fatalf("frozen branch = %q, want %q", frozen.Branch, testBranch)
	}

	retitled := f.request()
	retitled.Issue.Title = "Completely different title now!!!"
	retitled.Trigger.CommentID = testComment + 1
	second := f.execute(retitled)

	if second.Branch != testBranch {
		t.Errorf("second run branch = %q, want the frozen %q", second.Branch, testBranch)
	}
	if slug := f.reloadSession().FeatureSlug; slug != testSlug {
		t.Errorf("slug after the second run = %q, want the frozen %q", slug, testSlug)
	}
	runs := f.runs()
	if len(runs) != 2 || runs[1].Ordinal != 2 {
		t.Fatalf("runs = %+v, want two with ordinals 1 and 2", runs)
	}
}

// TestExecuteRefusesACorruptedFrozenSlug is the half of AC-3.7 that only a
// stored value can reach: the derivation is sound, but a hand-edited row or a
// botched migration is not, and the post-condition is what stops it becoming a
// branch name.
func TestExecuteRefusesACorruptedFrozenSlug(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran with an invalid feature slug")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	if _, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, "--not a slug--", "lf/123---not a slug--"); err != nil {
		t.Fatalf("seed a corrupted slug: %v", err)
	}

	result := f.execute(f.request())

	if result.Disposition != DispositionPrepareFailed {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionPrepareFailed)
	}
	if !strings.Contains(result.Reason, "slug") {
		t.Errorf("reason = %q, want it to name the slug", result.Reason)
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs, want 0", len(runs))
	}
}

// TestExecuteRefusesAnOccupiedWorktreePath is AC-3.9. Nothing at the path is
// deleted or overwritten to make room, and the file left there is the proof.
func TestExecuteRefusesAnOccupiedWorktreePath(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran with an occupied worktree path")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)

	dir := f.worktreeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the occupying directory: %v", err)
	}
	occupant := filepath.Join(dir, "someone-elses-work.txt")
	if err := os.WriteFile(occupant, []byte("do not delete me\n"), 0o600); err != nil {
		t.Fatalf("write the occupying file: %v", err)
	}

	result := f.execute(f.request())

	if result.Disposition != DispositionPrepareFailed {
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionPrepareFailed, f.logs)
	}
	if !errors.Is(result.Err, git.ErrWorktreeOccupied) {
		t.Errorf("Err = %v, want it to unwrap to %v", result.Err, git.ErrWorktreeOccupied)
	}
	if content, err := os.ReadFile(occupant); err != nil || string(content) != "do not delete me\n" {
		t.Errorf("the occupying file was disturbed: content=%q err=%v", content, err)
	}
}

// TestExecutePushFailurePreservesTheCommit is AC-3.24. The push is broken from
// inside the run, after the fetch has already succeeded, which is the only
// ordering that isolates the push.
func TestExecutePushFailurePreservesTheCommit(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		// The remote goes away between the fetch and the push.
		if err := os.RemoveAll(f.repo.Origin); err != nil {
			t.Fatalf("remove the origin: %v", err)
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	var pushErr *git.PushError
	switch {
	case result.Succeeded():
		t.Fatal("a turn whose push failed was reported as a success")
	case !result.Committed || result.CommitSHA == "":
		t.Fatalf("commit was lost: committed=%t sha=%q", result.Committed, result.CommitSHA)
	case result.Pushed:
		t.Error("Pushed = true after the remote went away")
	case !errors.As(result.Err, &pushErr):
		t.Fatalf("Err = %v, want a *git.PushError naming the worktree", result.Err)
	case pushErr.Worktree != result.WorktreePath:
		t.Errorf("push error worktree = %q, want %q", pushErr.Worktree, result.WorktreePath)
	}

	// The commit is where the report says it is.
	if head := f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"); head != result.CommitSHA {
		t.Errorf("worktree HEAD = %q, want the commit %q", head, result.CommitSHA)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].CommitSHA != result.CommitSHA {
		t.Errorf("runs = %+v, want the commit recorded on the row", runs)
	}
	if !result.Reportable() {
		t.Error("Reportable() = false: a failed push has to be told")
	}
}

// TestExecuteRecordsADistinctReasonPerOutcome is AC-3.20 and S-9. The five
// outcomes stay five: a timeout, a shutdown and an exit status of 2 call for
// three different responses, and an abandonment is not the run's fault at all.
func TestExecuteRecordsADistinctReasonPerOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		outcome    harness.Outcome
		exitCode   int
		reason     string
		wantStatus store.RunStatus
	}{
		{
			name:       "succeeded",
			outcome:    harness.OutcomeSucceeded,
			exitCode:   0,
			reason:     "",
			wantStatus: store.RunSucceeded,
		},
		{
			name:       "failed",
			outcome:    harness.OutcomeFailed,
			exitCode:   2,
			reason:     "claude exited with status 2",
			wantStatus: store.RunFailed,
		},
		{
			name:       "timed out",
			outcome:    harness.OutcomeTimedOut,
			exitCode:   harness.SignalExitCode,
			reason:     "timed out after 30m0s; the process subtree was killed",
			wantStatus: store.RunFailed,
		},
		{
			name:       "abandoned",
			outcome:    harness.OutcomeAbandoned,
			exitCode:   harness.SignalExitCode,
			reason:     "abandoned: the caller's run context ended before claude finished",
			wantStatus: store.RunFailed,
		},
		{
			name:       "no result",
			outcome:    harness.OutcomeNoResult,
			exitCode:   0,
			reason:     "claude exited 0 without emitting a terminal result event",
			wantStatus: store.RunFailed,
		},
	}

	seen := make(map[string]string, len(tests))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				result := succeeded(req, f.paths.Home())
				result.Outcome = tc.outcome
				result.ExitCode = tc.exitCode
				result.Reason = tc.reason
				result.HasResult = tc.outcome != harness.OutcomeNoResult
				return result, nil
			})
			f = newFixture(t, runner)

			result := f.execute(f.request())

			if result.Outcome != tc.outcome {
				t.Errorf("outcome = %s, want %s", result.Outcome, tc.outcome)
			}
			runs := f.runs()
			if len(runs) != 1 {
				t.Fatalf("runs = %d, want 1", len(runs))
			}
			run := runs[0]
			switch {
			case run.Status != tc.wantStatus:
				t.Errorf("run status = %q, want %q", run.Status, tc.wantStatus)
			case run.ExitCode == nil || *run.ExitCode != int64(tc.exitCode):
				t.Errorf("recorded exit code = %v, want %d", run.ExitCode, tc.exitCode)
			case run.Error != tc.reason:
				t.Errorf("recorded reason = %q, want %q", run.Error, tc.reason)
			}
			if meta := f.readMeta(1); meta.Outcome != tc.outcome.String() {
				t.Errorf("meta outcome = %q, want %q", meta.Outcome, tc.outcome)
			}

			// The whole point of five outcomes: no two of them record the same
			// thing, so a report can tell them apart without guessing.
			key := string(run.Status) + "/" + run.Error
			if other, ok := seen[key]; ok {
				t.Errorf("%s records the same status and reason as %s: %q", tc.name, other, key)
			}
			seen[key] = tc.name
		})
	}
}

// TestExecuteDoesNotCommitAnAbandonedRun is the D-B half of the abandonment
// outcome. The daemon has under three seconds to exit and a push takes up to
// two minutes; the work is safe in the worktree until the next run.
func TestExecuteDoesNotCommitAnAbandonedRun(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "half-done.go", "package api\n")
		result := succeeded(req, f.paths.Home())
		result.Outcome = harness.OutcomeAbandoned
		result.ExitCode = harness.SignalExitCode
		result.Reason = "abandoned: the daemon is shutting down"
		result.HasResult = false
		return result, nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	switch {
	case result.Outcome != harness.OutcomeAbandoned:
		t.Errorf("outcome = %s, want %s", result.Outcome, harness.OutcomeAbandoned)
	case result.Committed || result.Pushed:
		t.Errorf("committed=%t pushed=%t, want neither for an abandoned run", result.Committed, result.Pushed)
	}
	// The work is still there, uncommitted, for the next run to pick up.
	if _, err := os.Stat(filepath.Join(f.worktreeDir(), "half-done.go")); err != nil {
		t.Errorf("the abandoned run's work is gone: %v", err)
	}
	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after an abandoned run = %q, want it released", owner)
	}
}

// TestExecuteReportsAClaudeThatNeverStarted is AC-4.1 seen from below: a run
// row exists, so it owes the issue a comment even though there is no RunResult
// to render.
func TestExecuteReportsAClaudeThatNeverStarted(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, harness.ErrStartFailed
	})
	f := newFixture(t, runner)

	result := f.execute(f.request())

	switch {
	case result.Disposition != DispositionRan:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionRan)
	case !result.Reportable():
		t.Error("Reportable() = false: a run that never started still has a row and a comment to post")
	case result.Succeeded():
		t.Error("Succeeded() = true for a claude that never started")
	case !errors.Is(result.Err, harness.ErrStartFailed):
		t.Errorf("Err = %v, want %v", result.Err, harness.ErrStartFailed)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunFailed {
		t.Fatalf("runs = %+v, want one failed row", runs)
	}
	if runs[0].ExitCode == nil || *runs[0].ExitCode != store.SignalExitCode {
		t.Errorf("exit code = %v, want %d", runs[0].ExitCode, store.SignalExitCode)
	}
}

// TestExecuteResolvesTheAuthorFromTheClone covers the fallback: with no
// configured identity the clone answers, so a litefactory commit carries the
// same name and address the user's own commits do.
func TestExecuteResolvesTheAuthorFromTheClone(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.Author = git.Author{}
	f.repo.Git("config", "user.name", "Clone Configured")
	f.repo.Git("config", "user.email", "clone@example.invalid")

	result := f.execute(f.request())
	if !result.Committed {
		t.Fatalf("nothing was committed: %+v\nlogs:\n%s", result, f.logs)
	}

	author := f.repo.GitIn(f.worktreeDir(), "log", "-1", "--format=%an <%ae>")
	if author != "Clone Configured <clone@example.invalid>" {
		t.Errorf("commit author = %q, want the clone's identity", author)
	}
}

// TestExecuteValidatesItsArguments covers the one thing that is an error rather
// than a result: a caller that is wrong.
func TestExecuteValidatesItsArguments(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran for an invalid turn")
		return nil, errors.New("must not be reached")
	}))

	t.Run("options", func(t *testing.T) {
		t.Parallel()

		opts := f.opts
		opts.InstanceID = ""
		if _, err := Execute(t.Context(), opts, f.request()); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("err = %v, want %v", err, ErrInvalidOptions)
		}
	})

	t.Run("command that does not spawn a run", func(t *testing.T) {
		t.Parallel()

		req := f.request()
		req.Command = ParseCommand("/plan something")
		if _, err := Execute(t.Context(), f.opts, req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("err = %v, want %v", err, ErrInvalidRequest)
		}
	})

	t.Run("issue that is not the session's", func(t *testing.T) {
		t.Parallel()

		req := f.request()
		req.Issue.Number = testIssue + 1
		if _, err := Execute(t.Context(), f.opts, req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("err = %v, want %v", err, ErrInvalidRequest)
		}
	})

	// A trigger id of 0 is the issue body, not a missing comment (AC-2.16b).
	// It is the one non-positive value a turn accepts, and it is 0 rather than
	// the issue's own forge id because reporting the run advances the watermark
	// to it (AC-2.23).
	t.Run("issue body as the trigger", func(t *testing.T) {
		t.Parallel()

		req := f.request()
		req.Trigger.CommentID = 0
		if err := req.validate(); err != nil {
			t.Errorf("validate with a trigger of 0 = %v, want it accepted as the issue body", err)
		}

		req.Trigger.CommentID = -1
		if err := req.validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("validate with a trigger of -1 = %v, want %v", err, ErrInvalidRequest)
		}
	})
}

// TestReconcileReclaimsAndLogs is AC-3.2: a lock held by a non-current daemon
// instance is reclaimed at startup, and the reclaim is logged rather than
// silently swallowed.
func TestReconcileReclaimsAndLogs(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("must not be reached")
	}))
	acquired, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, testOtherOwner)
	if err != nil || !acquired {
		t.Fatalf("seed the lock: acquired=%t err=%v", acquired, err)
	}

	reconciliation, err := Reconcile(t.Context(), f.opts)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(reconciliation.ReclaimedLocks) != 1 {
		t.Fatalf("reclaimed %d locks, want 1", len(reconciliation.ReclaimedLocks))
	}
	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after reconciliation = %q, want it free", owner)
	}
	logs := f.logs.String()
	for _, want := range []string{"reclaimed a task lock", testOtherOwner, f.session.TaskRef} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs do not mention %q:\n%s", want, logs)
		}
	}
}

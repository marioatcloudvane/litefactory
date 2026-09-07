package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestSaveOnlyCommitsASucceededRun is US-1 (T-1.3, T-1.4, T-1.5): save's gate
// widened from "OutcomeAbandoned only" to "!runResult.Succeeded()", so that a
// failed, timed-out or no-result run leaves its worktree exactly as
// uncommitted as an abandoned one always did.
//
// AC-1.1, AC-1.2: only Succeeded reaches commitAndPush; the other four never
// move the branch and never push. AC-1.3: a succeeded run still commits and
// pushes with runResult.ResultText as the summary. AC-1.4: the run's own
// change is still on disk afterwards. AC-1.5: the run's row keeps an empty
// commit_sha and SetRunCommit is never reached (finish's CommitSHA branch is
// gated on result.CommitSHA being non-empty). AC-1.6: Changed, Committed,
// Pushed, CommitSHA, SHAAfter, Changes are all zero, and Err stays nil.
func TestSaveOnlyCommitsASucceededRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		outcome   harness.Outcome
		exitCode  int
		reason    string
		hasResult bool
	}{
		{
			name:      "succeeded",
			outcome:   harness.OutcomeSucceeded,
			exitCode:  0,
			hasResult: true,
		},
		{
			name:      "failed",
			outcome:   harness.OutcomeFailed,
			exitCode:  2,
			reason:    "claude exited with status 2",
			hasResult: true,
		},
		{
			name:      "timed out",
			outcome:   harness.OutcomeTimedOut,
			exitCode:  harness.SignalExitCode,
			reason:    "timed out after 30m0s; the process subtree was killed",
			hasResult: false,
		},
		{
			name:      "no result",
			outcome:   harness.OutcomeNoResult,
			exitCode:  0,
			reason:    "claude exited 0 without emitting a terminal result event",
			hasResult: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const fileName = "half-done.go"
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				writeInWorktree(t, req, fileName, "package api\n")
				result := succeeded(req, f.paths.Home())
				result.Outcome = tc.outcome
				result.ExitCode = tc.exitCode
				result.Reason = tc.reason
				result.HasResult = tc.hasResult
				return result, nil
			})
			f = newFixture(t, runner)

			result := f.execute(f.request())

			if tc.outcome == harness.OutcomeSucceeded {
				// AC-1.3: unchanged happy path.
				if !result.Succeeded() {
					t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
				}
				if !result.Committed || !result.Pushed || result.CommitSHA == "" {
					t.Errorf("committed=%t pushed=%t sha=%q, want all set", result.Committed, result.Pushed, result.CommitSHA)
				}
				if !f.originHas(testBranch) {
					t.Errorf("origin does not carry %s after a succeeded run", testBranch)
				}
				message := f.repo.GitIn(f.worktreeDir(), "log", "-1", "--format=%B")
				if !strings.Contains(message, result.Harness.ResultText) {
					t.Errorf("commit message does not carry the run's result text %q:\n%s", result.Harness.ResultText, message)
				}
				return
			}

			// AC-1.1/AC-1.2: no commit, no push, whatever the outcome.
			switch {
			case result.Succeeded():
				t.Fatalf("a %s run was reported as a success: %+v", tc.name, result)
			case result.Changed, result.Committed, result.Pushed:
				t.Errorf("changed=%t committed=%t pushed=%t, want none for a %s run",
					result.Changed, result.Committed, result.Pushed, tc.name)
			case result.CommitSHA != "":
				t.Errorf("CommitSHA = %q, want empty", result.CommitSHA)
			case result.SHAAfter != "":
				t.Errorf("SHAAfter = %q, want empty: the worktree was never inspected after a %s run", result.SHAAfter, tc.name)
			case len(result.Changes) != 0:
				t.Errorf("Changes = %v, want none", result.Changes)
			case result.Err != nil:
				t.Errorf("Err = %v, want nil: the run's own failure is not litefactory's", result.Err)
			}

			// AC-1.1: the branch never moved locally either, so there is
			// nothing a push could have sent and no commit message anywhere
			// could carry the run's result text.
			if head := f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"); head != result.SHABefore {
				t.Errorf("worktree HEAD moved to %q after a %s run; sha_before was %q", head, tc.name, result.SHABefore)
			}
			if f.originHas(testBranch) {
				t.Errorf("origin carries %s after a %s run; nothing should have been pushed", testBranch, tc.name)
			}

			// AC-1.4: the run's own work is still there, uncommitted.
			path := filepath.Join(f.worktreeDir(), fileName)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("the %s run's work is gone: %v", tc.name, err)
			}
			status := f.repo.GitIn(f.worktreeDir(), "status", "--porcelain")
			if !strings.Contains(status, fileName) {
				t.Errorf("git status does not show %s as changed after a %s run:\n%s", fileName, tc.name, status)
			}

			// T-1.3: the log names the worktree path rather than silently
			// dropping the work.
			logs := f.logs.String()
			if !strings.Contains(logs, "leaving the work uncommitted in the worktree") {
				t.Errorf("the log does not say the work was left uncommitted for a %s run:\n%s", tc.name, logs)
			}
			if !strings.Contains(logs, f.worktreeDir()) {
				t.Errorf("the log does not name the worktree %s for a %s run:\n%s", f.worktreeDir(), tc.name, logs)
			}

			// AC-1.5/T-1.5: finish wrote no commit_sha, and the store row
			// still has none either — SetRunCommit is unreachable when
			// result.CommitSHA is empty (harvest.go's finish).
			runs := f.runs()
			if len(runs) != 1 {
				t.Fatalf("runs = %d, want 1", len(runs))
			}
			if runs[0].CommitSHA != "" {
				t.Errorf("run row commit_sha = %q after a %s run, want empty", runs[0].CommitSHA, tc.name)
			}
			if runs[0].Status != store.RunFailed {
				t.Errorf("run row status = %q after a %s run, want %q", runs[0].Status, tc.name, store.RunFailed)
			}
		})
	}
}

// TestSaveDoesNotCommitAbandonedIsUnchanged is a lightweight companion to
// TestSaveOnlyCommitsASucceededRun: it confirms the pre-existing abandoned-run
// behaviour (already pinned by TestExecuteDoesNotCommitAnAbandonedRun in
// execute_test.go) is exercised through the same widened gate as the other
// four outcomes, closing the loop across all five (AC-1.2).
func TestSaveDoesNotCommitAbandonedIsUnchanged(t *testing.T) {
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
	case result.Succeeded():
		t.Fatalf("an abandoned run was reported as a success: %+v", result)
	case result.Committed, result.Pushed, result.Changed:
		t.Errorf("committed=%t pushed=%t changed=%t, want none for an abandoned run",
			result.Committed, result.Pushed, result.Changed)
	case result.Err != nil:
		t.Errorf("Err = %v, want nil", result.Err)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].CommitSHA != "" {
		t.Errorf("runs = %+v, want one row with no commit_sha", runs)
	}
}

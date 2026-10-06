package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestSaveCommitsImplementOnEveryOutcome is the integration test of the
// Implement save rule: whatever the run left in the worktree is committed and
// pushed to the run's own branch on every outcome, to a temporary local bare
// remote, and the commit message names the outcome and, when the run did not
// complete, the reason.
func TestSaveCommitsImplementOnEveryOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		outcome   harness.Outcome
		exitCode  int
		reason    string
		hasResult bool
		wantWord  string
	}{
		{name: "succeeded", outcome: harness.OutcomeSucceeded, hasResult: true, wantWord: "Implement run completed"},
		{name: "failed", outcome: harness.OutcomeFailed, exitCode: 2, reason: "claude exited with status 2", hasResult: true, wantWord: "Implement run failed: claude exited with status 2"},
		{name: "timed out", outcome: harness.OutcomeTimedOut, exitCode: harness.SignalExitCode, reason: "timed out after 30m0s", wantWord: "Implement run timed_out: timed out after 30m0s"},
		{name: "no result", outcome: harness.OutcomeNoResult, reason: "exited 0 without a terminal result", wantWord: "Implement run no_result: exited 0 without a terminal result"},
		{name: "interrupted", outcome: agent.OutcomeInterrupted, reason: "session limit", wantWord: "Implement run interrupted: session limit"},
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
				if tc.outcome == agent.OutcomeInterrupted {
					result.Interruption = &agent.Interruption{Precision: agent.ResetExact}
				}
				return result, nil
			})
			f = newFixture(t, runner)

			result := f.execute(f.request())

			if tc.outcome != harness.OutcomeSucceeded && result.Succeeded() {
				t.Fatalf("a %s run was reported as a success: %+v", tc.name, result)
			}
			if result.Err != nil {
				t.Fatalf("Err = %v, want nil: the work was saved", result.Err)
			}
			if !result.Committed || !result.Pushed || result.CommitSHA == "" {
				t.Fatalf("committed=%t pushed=%t sha=%q, want all set for a %s run\nlogs:\n%s", result.Committed, result.Pushed, result.CommitSHA, tc.name, f.logs)
			}
			if !f.originHas(testBranch) {
				t.Errorf("origin does not carry %s after a %s run", testBranch, tc.name)
			}
			if remote := f.repo.GitIn(f.repo.Origin, "rev-parse", "refs/heads/"+testBranch); remote != result.CommitSHA {
				t.Errorf("origin branch tip %q, want the run's commit %q", remote, result.CommitSHA)
			}
			message := f.repo.GitIn(f.worktreeDir(), "log", "-1", "--format=%B")
			if !strings.HasPrefix(message, tc.wantWord) {
				t.Errorf("commit message does not start with %q:\n%s", tc.wantWord, message)
			}
			if status := f.repo.GitIn(f.worktreeDir(), "status", "--porcelain"); strings.TrimSpace(status) != "" {
				t.Errorf("worktree is dirty after the save:\n%s", status)
			}
			runs := f.runs()
			if len(runs) != 1 || runs[0].CommitSHA != result.CommitSHA {
				t.Errorf("run row commit_sha = %+v, want %q", runs, result.CommitSHA)
			}
		})
	}
}

// TestSaveImplementNoChangesMakesNoCommit pins that an Implement run with an
// untouched worktree commits and pushes nothing, whatever its outcome.
func TestSaveImplementNoChangesMakesNoCommit(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		result := succeeded(req, f.paths.Home())
		result.Outcome = harness.OutcomeFailed
		result.ExitCode = 2
		result.Reason = "failed"
		return result, nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	if result.Committed || result.CommitSHA != "" || result.Pushed || result.Err != nil {
		t.Errorf("committed=%t sha=%q pushed=%t err=%v, want no commit, no push", result.Committed, result.CommitSHA, result.Pushed, result.Err)
	}
	if head := f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"); head != result.SHABefore {
		t.Errorf("HEAD moved to %q, sha_before %q", head, result.SHABefore)
	}
	if f.originHas(testBranch) {
		t.Errorf("origin carries %s although nothing changed", testBranch)
	}
}

// TestSaveImplementPushFailureKeepsOutcomeAndWork pins AC-006: a failed push is
// reported on the result, the run's own outcome stays as it was and the commit
// stays in the worktree. Nothing retries.
func TestSaveImplementPushFailureKeepsOutcomeAndWork(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "half-done.go", "package api\n")
		f.repo.SetOriginURL(filepath.Join(f.t.TempDir(), "missing.git"))
		result := succeeded(req, f.paths.Home())
		result.Outcome = harness.OutcomeFailed
		result.ExitCode = 2
		result.Reason = "claude exited with status 2"
		return result, nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	if result.Err == nil {
		t.Fatalf("a failed push was not reported: %+v", result)
	}
	if result.Outcome != harness.OutcomeFailed || result.Reason != "claude exited with status 2" {
		t.Errorf("outcome changed to %v / %q", result.Outcome, result.Reason)
	}
	if result.Pushed {
		t.Error("Pushed set although the push failed")
	}
	if !result.Committed || result.CommitSHA == "" {
		t.Errorf("the work was not committed locally: %+v", result)
	}
	if head := f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"); head != result.CommitSHA {
		t.Errorf("worktree HEAD %q, want the retained commit %q", head, result.CommitSHA)
	}
	if _, err := os.Stat(filepath.Join(f.worktreeDir(), "half-done.go")); err != nil {
		t.Errorf("the run's work is gone: %v", err)
	}
	if !strings.Contains(result.Publication, "retained") {
		t.Errorf("publication does not say the commit is retained: %q", result.Publication)
	}
}

// TestSaveRuleIsPerMode pins that only Implement commits on every outcome and
// that only an accepted completed answer counts as finished work.
func TestSaveRuleIsPerMode(t *testing.T) {
	t.Parallel()

	for mode, want := range map[string]bool{"implement": true, "plan": false, "research": false, "validate": false, "spike": false} {
		prep := &preparation{run: store.Run{Mode: mode}}
		if got := prep.alwaysCommits(); got != want {
			t.Errorf("alwaysCommits(%s) = %t, want %t", mode, got, want)
		}
	}
	if (&preparation{run: store.Run{Mode: "implement", Workflow: "bug"}}).alwaysCommits() {
		t.Error("a Bug workflow run commits on every outcome")
	}
	for outcome, want := range map[string]bool{"": true, "completed": true, "incomplete": false, "blocked": false} {
		if got := (&preparation{acceptedOutcome: outcome}).workComplete(); got != want {
			t.Errorf("workComplete(%q) = %t, want %t", outcome, got, want)
		}
	}
}

func TestImplementCommitSummary(t *testing.T) {
	t.Parallel()

	ok := &harness.RunResult{Outcome: harness.OutcomeSucceeded, ResultText: "Added it."}
	tests := []struct {
		name string
		prep preparation
		run  *harness.RunResult
		want string
	}{
		{"completed", preparation{acceptedOutcome: "completed"}, ok, "Implement run completed\n\nAdded it."},
		{"process success without accepted answer", preparation{}, ok, "Implement run completed\n\nAdded it."},
		{"incomplete names reason", preparation{acceptedOutcome: "incomplete", acceptedReason: "tests\nstill failing"}, ok, "Implement run incomplete: tests still failing\n\nAdded it."},
		{"blocked names reason", preparation{acceptedOutcome: "blocked", acceptedReason: "needs a decision"}, &harness.RunResult{Outcome: harness.OutcomeSucceeded}, "Implement run blocked: needs a decision"},
		{"timed out", preparation{}, &harness.RunResult{Outcome: harness.OutcomeTimedOut, Reason: "timed out"}, "Implement run timed_out: timed out"},
	}
	for _, tc := range tests {
		if got := implementCommitSummary(&tc.prep, tc.run); got != tc.want {
			t.Errorf("%s: summary %q, want %q", tc.name, got, tc.want)
		}
	}
}

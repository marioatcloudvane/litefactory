package bugchecks

// Integration coverage for story-003 (issue #427)'s sandboxed half: a real
// fixer self-check cycle — regression phase, then focused phase once
// regression passed — run through the real [Isolation] sandbox executor
// against a minimal fixture worktree, the same contained process a "factory"
// check uses. This mirrors sandbox_integration_test.go's fixture and skip
// convention: [Isolation] requires a real Codex CLI (LF_TEST_CODEX), and
// there is deliberately no unisolated fallback to exercise instead.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// TestBugChecksSandboxSelfCheckRunsRegressionThenFocusedPhase drives the same
// two-phase sequencing story-003's fixer self-check feature relies on
// (AC-002: focused only ever follows a passed regression, never the reverse)
// through the real sandbox, against a minimal fixture repository commit: a
// regression command that must see the repository's tracked file, followed
// by a focused command that only runs because the caller observed the first
// one pass — exactly the ordering [DB.StartBugSelfCheck] enforces at the
// store layer in internal/store/bug_self_checks_integration_test.go.
func TestBugChecksSandboxSelfCheckRunsRegressionThenFocusedPhase(t *testing.T) {
	p, _ := sandboxFixture(t)
	worktree := t.TempDir()

	// A minimal fixture repository commit: one tracked file the regression
	// phase's command depends on.
	if err := os.WriteFile(filepath.Join(worktree, "handler.go"), []byte("package handler\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	regression := config.BugCheckCommand{
		Argv:           []string{"/bin/sh", "-c", "test -f handler.go && echo regression-passed"},
		TimeoutSeconds: 30,
	}
	var regressionOut []CommandResult
	regressionResult := p.Run(t.Context(), worktree, regression, func(o CommandResult) error {
		regressionOut = append(regressionOut, o)
		return nil
	})
	if regressionResult.Outcome != "passed" {
		t.Fatalf("regression phase outcome %q, want passed: reason=%q output=%q", regressionResult.Outcome, regressionResult.Reason, regressionResult.Output)
	}
	if len(regressionOut) != 1 || !regressionOut[0].CleanupConfirmed {
		t.Fatalf("regression phase evidence incomplete: %+v", regressionOut)
	}

	// The focused phase only ever runs because the caller (the store layer,
	// in production) observed the regression phase's own passing evidence
	// first; this test drives that same sequencing, not the store's guard.
	focused := config.BugCheckCommand{
		Argv:           []string{"/bin/sh", "-c", "echo focused-narrowed-check"},
		TimeoutSeconds: 30,
	}
	focusedResult := p.Run(t.Context(), worktree, focused, func(CommandResult) error { return nil })
	if focusedResult.Outcome != "passed" {
		t.Fatalf("focused phase outcome %q, want passed: reason=%q output=%q", focusedResult.Outcome, focusedResult.Reason, focusedResult.Output)
	}

	// A self-check never gets the full phase: this fixture never runs one,
	// matching AC-002 ("the full phase can never be self-checked").
}

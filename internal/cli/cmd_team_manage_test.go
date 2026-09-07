package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
)

// writeManageOverrideSource writes a minimal valid agent markdown source at
// repoDir/relPath, creating any parent directories, so a test can point
// `team manage set` at a real file inside the target repo.
func writeManageOverrideSource(t *testing.T, repoDir, relPath string) {
	t.Helper()
	full := filepath.Join(repoDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := validTeamAgentSource("reviewer", "a patched reviewer agent", "Patched body.")
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestTeamManage_SetThenShow covers AC-1: `set` records an override that
// `show` then reports back for that repo and agent key.
func TestTeamManage_SetThenShow(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	writeManageOverrideSource(t, repo.Dir, "local/reviewer.md")

	router, stdout, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "set", "source=local/reviewer.md",
		"--repo", repo.Dir,
	})
	if code != ExitOK {
		t.Fatalf("set: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "override set: local/reviewer.md") {
		t.Errorf("set stdout = %q, want it to mention the recorded path", stdout)
	}

	router, stdout, stderr = newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "show",
		"--repo", repo.Dir,
	})
	if code != ExitOK {
		t.Fatalf("show: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "override: active") ||
		!strings.Contains(stdout.String(), "path: local/reviewer.md") {
		t.Errorf("show stdout = %q, want it to report the active override and its path", stdout)
	}
}

// TestTeamManage_SetThenUnset covers AC-2: `unset` removes the override, and
// a subsequent `show` reports no override in effect.
func TestTeamManage_SetThenUnset(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	writeManageOverrideSource(t, repo.Dir, "local/reviewer.md")

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "set", "source=local/reviewer.md",
		"--repo", repo.Dir,
	})
	if code != ExitOK {
		t.Fatalf("set: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	router, stdout, stderr := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "unset",
		"--repo", repo.Dir,
	})
	if code != ExitOK {
		t.Fatalf("unset: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "override unset") {
		t.Errorf("unset stdout = %q, want confirmation", stdout)
	}

	router, stdout, stderr = newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "show",
		"--repo", repo.Dir,
	})
	if code != ExitOK {
		t.Fatalf("show: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "override: none") {
		t.Errorf("show stdout after unset = %q, want it to report no override", stdout)
	}
}

// TestTeamManage_SetNonexistentPath_FailsWithPrecondition covers AC-4: a set
// naming a local path that doesn't exist fails with a precondition error and
// records no override — confirmed by a follow-up show reporting none active.
func TestTeamManage_SetNonexistentPath_FailsWithPrecondition(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "set", "source=local/missing.md",
		"--repo", repo.Dir,
	})
	if code != ExitPrecondition {
		t.Fatalf("set (missing path): exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}

	router, stdout, stderr := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "show",
		"--repo", repo.Dir,
	})
	if code != ExitOK {
		t.Fatalf("show: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "override: none") {
		t.Errorf("show stdout after rejected set = %q, want it to report no override recorded", stdout)
	}
}

// TestTeamManage_SetNonMarkdownPath_FailsWithPrecondition covers the other
// half of AC-4: a path that exists but is not a markdown file is also
// refused with a precondition error.
func TestTeamManage_SetNonMarkdownPath_FailsWithPrecondition(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	full := filepath.Join(repo.Dir, "local", "reviewer.txt")
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte("not markdown\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "manage", "reviewer", "set", "source=local/reviewer.txt",
		"--repo", repo.Dir,
	})
	if code != ExitPrecondition {
		t.Fatalf("set (non-markdown path): exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}
}

// TestTeamManage_InvalidAgentKey_FailsWithUsage covers the filename-safety
// requirement that every `team manage` action validates agent-key against
// team.KeyPattern uniformly before dispatch.
func TestTeamManage_InvalidAgentKey_FailsWithUsage(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "manage", "Not_Valid!", "show",
		"--repo", repo.Dir,
	})
	if code != ExitUsage {
		t.Fatalf("show (invalid agent key): exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
}

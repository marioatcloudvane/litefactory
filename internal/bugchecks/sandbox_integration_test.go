package bugchecks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// sandboxFixture needs a real Codex CLI: LF_TEST_CODEX names it, and
// LF_TEST_CODEX_PATH the directories it needs (node for the npm launcher).
// Without them the test is skipped; nothing runs unisolated in its place.
func sandboxFixture(t *testing.T) (Isolation, string) {
	t.Helper()
	binary := os.Getenv("LF_TEST_CODEX")
	if binary == "" {
		t.Skip("LF_TEST_CODEX is not set; no real sandbox available")
	}
	home := t.TempDir()
	secret := filepath.Join(home, "secret")
	if err := os.MkdirAll(secret, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "token"), []byte("canary-credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := "/usr/bin:/bin"
	if extra := os.Getenv("LF_TEST_CODEX_PATH"); extra != "" {
		path = extra + ":" + path
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	return Isolation{Binary: binary, PATH: path, Home: resolved, DenyRead: []string{filepath.Join(resolved, "secret")}, Revision: "fixture"}, resolved
}

func TestBugChecksSandboxRunsRecipeCommandContained(t *testing.T) {
	p, home := sandboxFixture(t)
	worktree := t.TempDir()
	script := `echo run; touch inside.txt || exit 9; ` +
		`touch "$HOME/outside.txt" 2>/dev/null && exit 10; ` +
		`cat "$HOME/secret/token" 2>/dev/null && exit 11; ` +
		`/usr/bin/curl -sS -m 5 https://example.com -o /dev/null 2>/dev/null && exit 12; ` +
		`touch "$TMPDIR/scratch" || exit 13; exit 0`
	var observed []CommandResult
	out := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", script}, TimeoutSeconds: 60}, func(o CommandResult) error {
		observed = append(observed, o)
		return nil
	})
	if out.Outcome != "passed" || out.Exit == nil || *out.Exit != 0 {
		t.Fatalf("contained command outcome %q exit %v reason %q output %q", out.Outcome, out.Exit, out.Reason, out.Output)
	}
	if len(observed) != 1 || observed[0].ProcessIdentity == "" || !out.CleanupConfirmed || !out.CaptureComplete {
		t.Fatalf("launch identity or cleanup not retained: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(worktree, "inside.txt")); err != nil {
		t.Fatal("worktree write did not land")
	}
	if _, err := os.Stat(filepath.Join(home, "outside.txt")); err == nil {
		t.Fatal("write outside the worktree landed")
	}
	if strings.Contains(out.Output, "canary-credential") {
		t.Fatal("unreadable secret leaked into check output")
	}
}

func TestBugChecksSandboxClassifiesFailureAndTimeout(t *testing.T) {
	p, _ := sandboxFixture(t)
	worktree := t.TempDir()
	noop := func(CommandResult) error { return nil }
	failed := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", "exit 1"}, TimeoutSeconds: 60}, noop)
	if failed.Outcome != "failed" {
		t.Fatalf("test failure classified %q: %s", failed.Outcome, failed.Reason)
	}
	infra := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", "exit 7"}, TimeoutSeconds: 60}, noop)
	if infra.Outcome != "blocked" {
		t.Fatalf("infrastructure exit classified %q", infra.Outcome)
	}
	slow := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", "sleep 30 & sleep 30"}, TimeoutSeconds: 1}, noop)
	if slow.Outcome != "blocked" || slow.Reason != "check timed out" || !slow.CleanupConfirmed {
		t.Fatalf("timeout outcome %q reason %q cleanup %v", slow.Outcome, slow.Reason, slow.CleanupConfirmed)
	}
}

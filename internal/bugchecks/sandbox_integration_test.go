package bugchecks

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// Offline checks cannot even use loopback; with the network grant, local test
// servers work.
func TestBugChecksSandboxLoopbackFollowsNetworkGrant(t *testing.T) {
	p, _ := sandboxFixture(t)
	listen := []string{"/usr/bin/python3", "-c", "import socket;s=socket.socket();s.bind(('127.0.0.1',0));s.listen();c=socket.socket();c.connect(s.getsockname())"}
	noop := func(CommandResult) error { return nil }
	offline := p.Run(t.Context(), t.TempDir(), config.BugCheckCommand{Argv: listen, TimeoutSeconds: 60}, noop)
	if offline.Outcome == "passed" {
		t.Fatal("offline check bound a loopback socket")
	}
	p.Network = true
	online := p.Run(t.Context(), t.TempDir(), config.BugCheckCommand{Argv: listen, TimeoutSeconds: 60}, noop)
	if online.Outcome != "passed" {
		t.Fatalf("network grant did not allow loopback: %s %s", online.Reason, online.Output)
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
	for _, code := range []string{"2", "7"} {
		got := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", "exit " + code}, TimeoutSeconds: 60}, noop)
		if got.Outcome != "failed" || got.Reason != "project check reported test failure" {
			t.Fatalf("exit %s classified %q: %s", code, got.Outcome, got.Reason)
		}
	}
	for _, code := range []string{"127", "137"} {
		infra := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", "exit " + code}, TimeoutSeconds: 60}, noop)
		if infra.Outcome != "blocked" {
			t.Fatalf("infrastructure exit %s classified %q", code, infra.Outcome)
		}
	}
	// Both children outlive the startup-inclusive timeout. Record their PIDs
	// before waiting, so a timeout before the fixture starts cannot pass.
	script := `sleep 120 & child=$!; echo $child > child.pid; echo $$ > parent.pid; /bin/ps -o pgid= -p "$child" > child.pgid; /bin/ps -o pgid= -p $$ > parent.pgid; echo ready > ready; wait`
	slow := p.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/sh", "-c", script}, TimeoutSeconds: 10}, noop)
	if slow.Outcome != "blocked" || slow.Reason != "check timed out" || !slow.CleanupConfirmed {
		t.Fatalf("timeout outcome %q reason %q cleanup %v", slow.Outcome, slow.Reason, slow.CleanupConfirmed)
	}
	ready, err := os.ReadFile(filepath.Join(worktree, "ready"))
	if err != nil || strings.TrimSpace(string(ready)) != "ready" {
		t.Fatalf("timeout fixture not ready: %q %v", ready, err)
	}
	for _, name := range []string{"child.pid", "parent.pid", "child.pgid", "parent.pgid"} {
		data, err := os.ReadFile(filepath.Join(worktree, name))
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			t.Fatal(fmt.Errorf("invalid %s: %q", name, data))
		}
		if strings.HasSuffix(name, ".pid") {
			if err := syscall.Kill(pid, 0); err == nil {
				t.Errorf("%s %d survived timeout", name, pid)
			}
		}
	}

}

// Strict resolution must inspect every ancestor; the old placement is the
// failing control, while both sibling roots retain the same content denial.
func TestBugChecksSandboxScratchSiblings(t *testing.T) {
	p, parent := sandboxFixture(t)
	paths := config.NewPaths(filepath.Join(parent, "factory"))
	for _, file := range []string{paths.DBPath(), filepath.Join(parent, "protected", "token")} {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("scratch-secret-canary"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p.DenyRead = []string{paths.Home(), filepath.Join(parent, "protected")}
	for _, tc := range []struct {
		name, root string
		allowed    bool
	}{
		{"suite", paths.ValidateScratchDir(), true},
		{"implement", paths.ImplementScratchDir(), true},
		{"old", filepath.Join(paths.Home(), "validate-scratch"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(tc.root, "operation", "tree")
			if err := os.MkdirAll(filepath.Join(root, "child"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("child", filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			script := `import pathlib,sys
for secret in sys.argv[2:]:
 try:
  with open(secret, "rb") as f: f.read()
 except PermissionError: pass
 else: sys.exit(10)
try: pathlib.Path(sys.argv[1]).resolve(strict=True)
except PermissionError:
 print("resolution-denied")
 sys.exit(1)
print("resolution-ok")
`
			out := p.Run(t.Context(), root, config.BugCheckCommand{Argv: []string{"/usr/bin/python3", "-c", script, filepath.Join(root, "link"), paths.DBPath(), filepath.Join(parent, "protected", "token")}, TimeoutSeconds: 60}, func(CommandResult) error { return nil })
			if tc.allowed {
				if out.Outcome != "passed" || !strings.Contains(out.Output, "resolution-ok") {
					t.Fatalf("sibling resolution: %+v", out)
				}
			} else if out.Outcome != "failed" || !strings.Contains(out.Output, "resolution-denied") {
				t.Fatalf("old placement did not fail ancestor resolution: %+v", out)
			}
			if strings.Contains(out.Output, "scratch-secret-canary") {
				t.Fatal("canary leaked")
			}
		})
	}
}

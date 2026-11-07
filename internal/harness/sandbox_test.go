package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// A read-only run (here a Spike investigator) holds the whole tool set, so
// its Bash commands run in an OS sandbox in which the worktree and $LF_HOME
// are unwritable and the network is closed. Caches and temporary files go to
// a private directory outside both, which is removed afterwards.
func TestReadOnlyRunIsSandboxedOutsideTheWorktree(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.runner.tools = config.AgentTools()
	req := f.request()
	req.Permissions = Permissions{Profile: "spike", Payload: ToolSurface{ReadOnly: true}}
	result, err := f.runner.Run(t.Context(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}

	argv := strings.Split(strings.TrimSpace(f.dump(t, "argv.dump")), "\n")
	value := func(flag string) string {
		if i := slices.Index(argv, flag); i >= 0 && i+1 < len(argv) {
			return argv[i+1]
		}
		return ""
	}
	if got := value("--permission-mode"); got != PermissionMode {
		t.Fatalf("--permission-mode = %q", got)
	}
	if got, want := value("--allowedTools"), strings.Join(config.AgentTools(), ","); got != want {
		t.Fatalf("--allowedTools = %q, want %q", got, want)
	}
	settingsPath := value("--settings")
	if settingsPath == f.runner.settingsPath || strings.HasPrefix(settingsPath, f.worktree) {
		t.Fatalf("--settings %q is not the run's own document outside the worktree", settingsPath)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read run settings: %v", err)
	}
	var doc struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
		Sandbox struct {
			Enabled           bool `json:"enabled"`
			FailIfUnavailable bool `json:"failIfUnavailable"`
			Unsandboxed       bool `json:"allowUnsandboxedCommands"`
			AutoAllow         bool `json:"autoAllowBashIfSandboxed"`
			Filesystem        struct {
				AllowWrite []string `json:"allowWrite"`
				DenyWrite  []string `json:"denyWrite"`
				DenyRead   []string `json:"denyRead"`
			} `json:"filesystem"`
			Network struct {
				Denied []string `json:"deniedDomains"`
			} `json:"network"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode run settings: %v", err)
	}
	sb := doc.Sandbox
	if !sb.Enabled || !sb.FailIfUnavailable || sb.Unsandboxed || !sb.AutoAllow {
		t.Fatalf("sandbox is not enforced: %+v", sb)
	}
	if !slices.Contains(sb.Filesystem.DenyWrite, f.worktree) || !slices.Contains(sb.Filesystem.DenyWrite, f.home) {
		t.Fatalf("worktree or home writable: %v", sb.Filesystem.DenyWrite)
	}
	for _, path := range sb.Filesystem.AllowWrite {
		if strings.HasPrefix(path, f.worktree) || strings.HasPrefix(path, f.home+string(filepath.Separator)) {
			t.Fatalf("writable path %q is inside the worktree or home", path)
		}
	}
	if !slices.Contains(sb.Filesystem.DenyRead, filepath.Join(f.home, "config.yaml")) {
		t.Fatalf("config.yaml readable: %v", sb.Filesystem.DenyRead)
	}
	if !slices.Equal(sb.Network.Denied, []string{"*"}) {
		t.Fatalf("network open: %v", sb.Network.Denied)
	}
	if !slices.Contains(doc.Permissions.Deny, "Bash(git push:*)") {
		t.Fatalf("operator deny rules lost: %v", doc.Permissions.Deny)
	}

	env := map[string]string{}
	for _, line := range strings.Split(f.dump(t, "env.dump"), "\n") {
		if name, val, ok := strings.Cut(line, "="); ok {
			env[name] = val
		}
	}
	for _, name := range []string{"GOCACHE", "TMPDIR"} {
		if !slices.Contains(sb.Filesystem.AllowWrite, env[name]) {
			t.Fatalf("%s=%q is not a sandbox-writable directory %v", name, env[name], sb.Filesystem.AllowWrite)
		}
	}
	if _, err := os.Stat(env["TMPDIR"]); !os.IsNotExist(err) {
		t.Fatalf("private temporary directory survived the run: %v", err)
	}
}

// A writable run that holds Bash is sandboxed too: it may write its worktree,
// its git directory and its submission directory and reach the allowlisted
// hosts, while $LF_HOME's state and every credential store stay unreadable
// and unwritable.
func TestWritableRunIsSandboxedWithCredentialsDenied(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.runner.tools = config.AgentTools()
	gitDir := filepath.Join(t.TempDir(), "gitdir")
	if err := os.WriteFile(filepath.Join(f.worktree, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	submissionRoot := filepath.Join(f.runner.paths.SubmissionDir(), "attempt")
	req := f.request()
	req.SubmissionRoot = submissionRoot
	req.Permissions = Permissions{Profile: "implement", Payload: ToolSurface{}}
	if result, err := f.runner.Run(t.Context(), req); err != nil || result.Outcome != OutcomeSucceeded {
		t.Fatalf("Run: %+v %v", result, err)
	}

	argv := strings.Split(strings.TrimSpace(f.dump(t, "argv.dump")), "\n")
	i := slices.Index(argv, "--settings")
	if i < 0 || argv[i+1] == f.runner.settingsPath || strings.HasPrefix(argv[i+1], f.worktree) {
		t.Fatalf("--settings is not the run's own document outside the worktree: %v", argv)
	}
	raw, err := os.ReadFile(argv[i+1])
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
		Sandbox struct {
			Enabled     bool `json:"enabled"`
			Fail        bool `json:"failIfUnavailable"`
			Unsandboxed bool `json:"allowUnsandboxedCommands"`
			Filesystem  struct {
				AllowWrite []string `json:"allowWrite"`
				DenyWrite  []string `json:"denyWrite"`
				DenyRead   []string `json:"denyRead"`
				AllowRead  []string `json:"allowRead"`
			} `json:"filesystem"`
			Network struct {
				Allowed []string `json:"allowedDomains"`
				Sockets []string `json:"allowUnixSockets"`
			} `json:"network"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sb := doc.Sandbox
	if !sb.Enabled || !sb.Fail || sb.Unsandboxed {
		t.Fatalf("sandbox is not enforced: %+v", sb)
	}
	for _, path := range append(f.runner.paths.DoctorCredentialPaths(f.runner.osHome), f.runner.paths.LogsDir(), f.runner.paths.DBPath()) {
		if !slices.Contains(sb.Filesystem.DenyRead, path) {
			t.Errorf("%s is readable: %v", path, sb.Filesystem.DenyRead)
		}
		if path != f.runner.paths.SubmissionDir() && !slices.Contains(sb.Filesystem.DenyWrite, path) {
			t.Errorf("%s is writable: %v", path, sb.Filesystem.DenyWrite)
		}
	}
	for _, path := range []string{filepath.Join(f.runner.osHome, ".ssh"), filepath.Join(f.runner.osHome, ".config", "gh"), filepath.Join(f.home, "config.yaml")} {
		if !slices.Contains(sb.Filesystem.DenyRead, path) {
			t.Errorf("credential path %s is readable", path)
		}
	}
	for _, path := range []string{f.worktree, gitDir, submissionRoot} {
		if !slices.Contains(sb.Filesystem.AllowWrite, path) {
			t.Errorf("%s is not writable: %v", path, sb.Filesystem.AllowWrite)
		}
	}
	if !slices.Contains(sb.Filesystem.AllowRead, submissionRoot) || !slices.Contains(sb.Network.Sockets, filepath.Join(submissionRoot, "s")) {
		t.Errorf("submission directory unreachable: %+v %+v", sb.Filesystem, sb.Network)
	}
	for _, host := range []string{"github.com", "proxy.golang.org", "registry.npmjs.org", "pypi.org"} {
		if !slices.Contains(sb.Network.Allowed, host) {
			t.Errorf("%s is not reachable: %v", host, sb.Network.Allowed)
		}
	}
	if !slices.Contains(doc.Permissions.Deny, "Bash(git push:*)") {
		t.Fatalf("operator deny rules lost: %v", doc.Permissions.Deny)
	}
}

// A run without a shell keeps the operator settings and uses the main cache.
func TestOrdinaryRunIsNotSandboxed(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.run(t)
	argv := strings.Split(strings.TrimSpace(f.dump(t, "argv.dump")), "\n")
	if i := slices.Index(argv, "--settings"); i < 0 || argv[i+1] != f.runner.settingsPath {
		t.Fatalf("--settings changed for an ordinary run: %v", argv)
	}
	env := f.dump(t, "env.dump")
	if !strings.Contains(env, "GOCACHE="+f.runner.goBuildCaches.Main+"\n") {
		t.Fatal("ordinary run did not receive the dedicated main cache")
	}
	if strings.Contains(env, "GOCACHE="+f.runner.goBuildCaches.Reviewer+"\n") || strings.Contains(env, filepath.Join(f.runner.paths.ReviewCacheDir(), "tmp")) {
		t.Fatal("ordinary run received reviewer cache or private temporary directory")
	}
}

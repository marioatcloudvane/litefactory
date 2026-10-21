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

// A read-only shell run (here a Spike investigator) may run the project's
// checks, so its Bash commands run in an OS sandbox in which the worktree and
// $LF_HOME are unwritable and the network is closed. Caches and temporary
// files go to a private directory outside both, which is removed afterwards.
func TestReadOnlyShellRunIsSandboxedOutsideTheWorktree(t *testing.T) {
	surfaceTools := []string{"Read", "Glob", "Grep", "Bash"}
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.Tools, c.ToolCeiling, c.AllowedTools = surfaceTools, surfaceTools, []string{"Read"}
	})
	req := f.request()
	req.Permissions = Permissions{Profile: "spike", Payload: ToolSurface{
		Tools: surfaceTools, AllowedTools: append([]string{"Read", "Glob", "Grep"}, config.ValidateReviewerShellRules()...), ReadOnlyShell: true,
	}}
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
	if got := value("--permission-mode"); got != ReadOnlyShellPermissionMode {
		t.Fatalf("--permission-mode = %q", got)
	}
	if !strings.Contains(value("--allowedTools"), "Bash(go test:*)") {
		t.Fatalf("the investigator cannot run the tests: %s", value("--allowedTools"))
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
	if !sb.Enabled || !sb.FailIfUnavailable || sb.Unsandboxed || sb.AutoAllow {
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

// An ordinary run keeps the operator's settings file and its own environment.
func TestOrdinaryRunIsNotSandboxed(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.run(t)
	argv := strings.Split(strings.TrimSpace(f.dump(t, "argv.dump")), "\n")
	if i := slices.Index(argv, "--settings"); i < 0 || argv[i+1] != f.runner.settingsPath {
		t.Fatalf("--settings changed for an ordinary run: %v", argv)
	}
	if strings.Contains(f.dump(t, "env.dump"), "GOCACHE=") {
		t.Fatal("ordinary run got the read-only shell cache")
	}
}

// The runner refuses a read-only shell that edits or runs a rule outside the
// fixed list, whatever mode carries it.
func TestReadOnlyShellRefusesWritingSurfaces(t *testing.T) {
	r := &Runner{}
	for name, surface := range map[string]ToolSurface{
		"editing tool": {Tools: []string{"Read", "Bash", "Edit"}, AllowedTools: []string{"Read"}, ReadOnlyShell: true},
		"go run":       {Tools: []string{"Read", "Bash"}, AllowedTools: []string{"Read", "Bash(go run:*)"}, ReadOnlyShell: true},
		"in-place sed": {Tools: []string{"Read", "Bash"}, AllowedTools: []string{"Read", "Bash(sed:*)"}, ReadOnlyShell: true},
	} {
		err := r.validateRequest(Request{Mode: "spike", Permissions: Permissions{Profile: "spike", Payload: surface}})
		if err == nil || !strings.Contains(err.Error(), "read-only shell cannot grant") {
			t.Errorf("%s: accepted: %v", name, err)
		}
	}
}

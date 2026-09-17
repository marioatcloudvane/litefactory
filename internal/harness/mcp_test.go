package harness

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func mcpTestSpec(t *testing.T, dir string) *agent.MCPLaunchSpec {
	t.Helper()
	spec, err := agent.NewMCPLaunchSpec(filepath.Join(dir, "factory 'quoted' ü $(touch forbidden);\\binary"), filepath.Join(dir, "descriptor.json"), "attempt-test", "communication_preflight", "communication_submit", "communication_receipt")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestPrepareMCPPrivateConfiguration(t *testing.T) {
	t.Parallel()
	f := newFixture(t, successScript(t))
	if _, err := EnsureSettings(f.runner.settingsPath, f.home); err != nil {
		t.Fatal(err)
	}
	hostileDir := filepath.Join(f.home, "quoted 'ü' \\ path\n$(touch forbidden);")
	if err := os.Mkdir(hostileDir, 0700); err != nil {
		t.Fatal(err)
	}
	spec := mcpTestSpec(t, hostileDir)
	path, cleanup, err := f.runner.prepareMCP(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	for _, tc := range []struct {
		path string
		mode os.FileMode
	}{{path, 0600}, {filepath.Dir(path), 0700}} {
		info, err := os.Stat(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != tc.mode {
			t.Errorf("permissions %s = %o, want %o", tc.path, info.Mode().Perm(), tc.mode)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	server, ok := decoded.Servers["litefactory"]
	if !ok || len(decoded.Servers) != 1 {
		t.Fatalf("servers = %v", decoded.Servers)
	}
	if server.Type != "stdio" || server.Command != spec.Executable() || !slices.Equal(server.Args, []string{"mcp", "serve", "--descriptor", filepath.Join(hostileDir, "descriptor.json")}) || server.Env == nil || len(server.Env) != 0 {
		t.Fatalf("unexpected server: %+v", server)
	}
	other, cleanupOther, err := f.runner.prepareMCP(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanupOther(); err != nil {
			t.Error(err)
		}
	}()
	if filepath.Dir(other) == filepath.Dir(path) {
		t.Fatal("parallel configurations share a directory")
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration retained: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("other attempt removed: %v", err)
	}
}

const captureMCP = `prev=''
for arg in "$@"; do
 if [ "$prev" = '--mcp-config' ]; then
  printf '%s' "$arg" > mcp-path.dump
  /bin/cat "$arg" > mcp-config.dump || exit 91
 fi
 prev="$arg"
done
`

func TestRunMCPNativeSurfaceAndLifetime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     RunPath
		validate bool
		enabled  bool
	}{
		{"cold", PathCold, false, true}, {"resume", PathResume, false, true}, {"fallback", PathColdAfterResumeRefused, false, true}, {"validate", PathCold, true, true}, {"absent", PathCold, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, captureMCP+successScript(t))
			req := f.request()
			req.Path = tc.path
			if tc.enabled {
				req.MCPLaunch = mcpTestSpec(t, f.home)
			}
			wantNative, wantAllowed := "Read,Edit", "Read,Edit(./**)"
			if tc.validate {
				req.Mode = "validate"
				req.Permissions = Permissions{Profile: "validate", Payload: ToolSurface{Tools: []string{"Read"}, AllowedTools: []string{"Read"}}}
				wantNative, wantAllowed = "Read", "Read"
			}
			// Ambient MCP configuration must never enter the generated registration.
			if err := os.WriteFile(filepath.Join(f.worktree, ".mcp.json"), []byte(`{"mcpServers":{"ambient":{"command":"untrusted"}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := f.runner.Run(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != OutcomeSucceeded {
				t.Fatalf("outcome %s", result.Outcome)
			}
			argv := strings.Split(strings.TrimSpace(f.dump(t, "argv.dump")), "\n")
			flag := func(name string) string {
				t.Helper()
				i := slices.Index(argv, name)
				if i < 0 || i+1 >= len(argv) {
					t.Fatalf("missing %s in %q", name, argv)
				}
				return argv[i+1]
			}
			if flag("--tools") != wantNative {
				t.Fatalf("native ceiling changed: %q", argv)
			}
			if tc.enabled {
				wantAllowed += ",mcp__litefactory__communication_preflight,mcp__litefactory__communication_submit,mcp__litefactory__communication_receipt"
			}
			if flag("--allowedTools") != wantAllowed {
				t.Fatalf("authorization = %q", flag("--allowedTools"))
			}
			if !slices.Contains(argv, "--strict-mcp-config") {
				t.Fatal("strict isolation missing")
			}
			if tc.path == PathResume {
				if flag("--resume") != testSessionID {
					t.Fatal("wrong resume identity")
				}
			} else if flag("--session-id") != testSessionID {
				t.Fatal("wrong cold identity")
			}
			if tc.enabled {
				path := f.dump(t, "mcp-path.dump")
				if flag("--mcp-config") != path {
					t.Fatal("config mismatch")
				}
				if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("config retained after exit: %v", err)
				}
				if strings.Contains(f.dump(t, "mcp-config.dump"), "ambient") {
					t.Fatal("ambient server included")
				}
			} else if slices.Contains(argv, "--mcp-config") {
				t.Fatal("absent registration changed invocation")
			}
		})
	}
}

func TestRunMCPFailureCleanup(t *testing.T) {
	for _, name := range []string{"launch-error", "cancelled", "host-error"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, captureMCP+dumpPrologue+"exit 17\n")
			req := f.request()
			req.MCPLaunch = mcpTestSpec(t, f.home)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "launch-error" {
				if err := os.Remove(filepath.Join(f.binDir, "claude")); err != nil {
					t.Fatal(err)
				}
			}
			if name == "cancelled" {
				cancel()
			}
			result, err := f.runner.Run(ctx, req)
			if name == "launch-error" && err == nil {
				t.Fatal("missing launch error")
			}
			if name == "host-error" && (result == nil || result.Outcome != OutcomeFailed) {
				t.Fatalf("result = %+v, error %v", result, err)
			}
			matches, globErr := filepath.Glob(filepath.Join(f.home, "claude-mcp-*"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(matches) != 0 {
				t.Fatalf("private configs leaked: %v", matches)
			}
		})
	}
}

func TestRunMCPRefusesCollisionAndInvalidRegistration(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		invalid        bool
	}{
		{"trusted-name-collision", `{"mcpServers":{"litefactory":{"command":"attacker"}}}`, false},
		{"malformed-settings", `{"mcpServers":`, false},
		{"invalid-registration", `{}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, successScript(t))
			req := f.request()
			req.MCPLaunch = mcpTestSpec(t, f.home)
			if tc.invalid {
				req.MCPLaunch = &agent.MCPLaunchSpec{}
			}
			if err := os.WriteFile(f.runner.settingsPath, []byte(tc.settings), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.runner.Run(t.Context(), req); err == nil {
				t.Fatal("unsafe registration launched")
			}
			if _, err := os.Stat(filepath.Join(f.worktree, "argv.dump")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("host launched: %v", err)
			}
			matches, err := filepath.Glob(filepath.Join(f.home, "claude-mcp-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("partial setup leaked: %v", matches)
			}
		})
	}
}

func TestRunMCPActiveCancellation(t *testing.T) {
	t.Parallel()
	f := newFixture(t, captureMCP+"printf x > ready.fifo\n/bin/cat < wait.fifo\n")
	for _, name := range []string{"ready.fifo", "wait.fifo"} {
		if err := syscall.Mkfifo(filepath.Join(f.worktree, name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := os.OpenFile(filepath.Join(f.worktree, "ready.fifo"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ready.Close(); err != nil {
			t.Error(err)
		}
	}()
	req := f.request()
	req.MCPLaunch = mcpTestSpec(t, f.home)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type response struct {
		result *RunResult
		err    error
	}
	done := make(chan response, 1)
	go func() { result, err := f.runner.Run(ctx, req); done <- response{result, err} }()
	observed := make(chan error, 1)
	go func() { var b [1]byte; _, err := io.ReadFull(ready, b[:]); observed <- err }()
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case <-t.Context().Done():
		t.Fatal("fake host did not announce readiness")
	}
	path := f.dump(t, "mcp-path.dump")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config unavailable during launch: %v", err)
	}
	cancel()
	got := <-done
	if got.result == nil || got.result.Outcome != OutcomeAbandoned {
		t.Fatalf("cancellation result %+v, error %v", got.result, got.err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config retained after active cancellation: %v", err)
	}
}

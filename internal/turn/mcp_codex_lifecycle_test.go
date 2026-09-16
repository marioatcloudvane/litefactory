package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/submission"
)

// This file extends the US-005 T-002 production-seam lifecycle coverage
// (mcp_runtime_lifecycle_test.go) to the US-003 Codex path, which that run
// left deferred. Every test here drives the real internal/turn preparation
// and the real internal/submission broker exactly as the existing
// lifecycleMCPHost-based tests do; the only difference is that prep.runner is
// a real *codex.Runner pointed at a fake `codex` binary on a test-controlled
// PATH, rather than an in-process closure. That fake binary is a subprocess,
// so this exercises the real internal/codex argv/collision/quoting logic
// (Runner.args, Runner.mcpOverrides, Runner.checkMCPCollision) that a
// provider-neutral fake can never reach, while codexHost — a thin wrapper
// around the same real *codex.Runner — reuses lifecycleMCPProbe exactly as
// the other providers' fake hosts do, so the live broker is still verified
// from inside the one interface runMCPBound actually calls.

// codexHost wraps a real *codex.Runner and captures the registration it was
// actually launched with, so a test can inspect it (or probe the still-live
// broker through it) around the real subprocess call rather than only after
// the fact. SupportsMCP is promoted from the embedded *codex.Runner.
type codexHost struct {
	*codex.Runner
	before func(ctx context.Context, spec *agent.MCPLaunchSpec) error
	spec   *agent.MCPLaunchSpec
}

func (h *codexHost) Run(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
	h.spec = req.MCPLaunch
	if h.before != nil {
		if err := h.before(ctx, req.MCPLaunch); err != nil {
			return nil, err
		}
	}
	return h.Runner.Run(ctx, req)
}

// realCodexRunner builds a real *codex.Runner whose binary is a fake shell
// script on a private PATH, mirroring internal/turn/harness_test.go's
// realRunner and internal/codex/runner_test.go's fakeRunner for the other
// provider's own real subprocess.
func realCodexRunner(t *testing.T, home, script string, adjust ...func(*codex.Options)) *codex.Runner {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\n"+script), 0o700); err != nil { //nolint:gosec // a fixture executable.
		t.Fatal(err)
	}
	opts := codex.Options{
		Paths:   config.NewPaths(home),
		Codex:   config.Codex{Binary: "codex", TimeoutSeconds: 30},
		Model:   "gpt-test",
		Environ: []string{"PATH=" + binDir + ":/usr/bin:/bin", "HOME=" + home, "USER=lf-test-user", "SHELL=/bin/sh"},
		Logger:  discardLogger(),
	}
	for _, fn := range adjust {
		fn(&opts)
	}
	runner, err := codex.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

// codexArgvDumpScript is the fake codex: it captures the exact argv it
// received (one element per line, byte-for-byte, so quoted/escaped values
// survive) and then emits a minimal successful turn.
func codexArgvDumpScript(dumpPath string) string {
	return fmt.Sprintf(`cat > /dev/null
printf '%%s\n' "$@" > %s
printf '%%s\n' '{"type":"thread.started","thread_id":"codex-fake-thread"}'
printf '%%s\n' '{"type":"turn.started"}'
printf '%%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"codex fake result"}}'
printf '%%s\n' '{"type":"turn.completed"}'
`, shellQuote(dumpPath))
}

// codexResumeRefusedScript captures argv and then exits non-zero having
// emitted no events at all, which is exactly the shape [codex.Runner.Run]
// classifies as ErrSessionNotResumable.
func codexResumeRefusedScript(dumpPath string) string {
	return fmt.Sprintf(`cat > /dev/null
printf '%%s\n' "$@" > %s
exit 7
`, shellQuote(dumpPath))
}

// codexReadyWaitScript announces readiness on a fifo and then blocks on a
// second one until released or killed, mirroring
// internal/harness/mcp_test.go's TestRunMCPActiveCancellation fixture.
const codexReadyWaitScript = `printf x > ready.fifo
/bin/cat < wait.fifo > /dev/null
printf '%s\n' '{"type":"turn.completed"}'
`

// shellQuote produces a single-quoted POSIX shell word, escaping any
// embedded single quote. dumpPath is always this test's own temp directory,
// never adversarial input, but this keeps the fixture scripts honest.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// codexLifecycleRequestIn builds a valid agent.Request addressed to prep's
// existing ref, with an explicit worktree and run ordinal so callers that
// need FIFOs in a known directory, or distinct ordinals across sibling runs
// of the same ref, can supply them.
func codexLifecycleRequestIn(t *testing.T, prep *preparation, mode, worktree string, ordinal int) agent.Request {
	t.Helper()
	sandbox := "workspace-write"
	if mode == "validate" {
		sandbox = "read-only"
	}
	return agent.Request{
		Ref: prep.ref, RunOrdinal: ordinal, SegmentOrdinal: 1,
		Session:      agent.SessionRef{Ref: "codex-lifecycle-session"},
		Mode:         mode,
		Path:         agent.PathCold,
		WorktreePath: worktree,
		Prompt:       "do the task",
		Permissions:  agent.Permissions{Profile: mode, Payload: codex.Sandbox{Mode: sandbox}},
	}
}

func codexLifecycleRequest(t *testing.T, prep *preparation, mode string, ordinal int) agent.Request {
	t.Helper()
	return codexLifecycleRequestIn(t, prep, mode, t.TempDir(), ordinal)
}

func readArgvDump(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func argvFlagValue(argv []string, name string) (string, bool) {
	for i, a := range argv {
		if a == name && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

// codexMCPOverrides is the decoded shape of the mcp_servers.litefactory.* -c
// overrides internal/codex/mcp.go emits, recovered from a captured argv.
type codexMCPOverrides struct {
	command string
	args    []string
	env     string
	enabled []string
}

// decodeCodexMCPOverrides finds every "-c mcp_servers.litefactory.<key>=<value>"
// pair in argv and decodes the quoted Go-syntax values
// internal/codex/mcp.go's strconv.Quote calls produced, proving the exact
// bytes it encoded are the exact bytes recovered here. Returns nil when no
// such override is present (disabled/absent registration).
func decodeCodexMCPOverrides(t *testing.T, argv []string) *codexMCPOverrides {
	t.Helper()
	const prefix = "mcp_servers.litefactory."
	values := map[string]string{}
	for i, a := range argv {
		if a != "-c" || i+1 >= len(argv) {
			continue
		}
		key, value, ok := strings.Cut(argv[i+1], "=")
		if !ok || !strings.HasPrefix(key, prefix) {
			continue
		}
		values[strings.TrimPrefix(key, prefix)] = value
	}
	if len(values) == 0 {
		return nil
	}
	o := &codexMCPOverrides{env: values["env"]}
	if v, ok := values["command"]; ok {
		s, err := strconv.Unquote(v)
		if err != nil {
			t.Fatalf("decode command override %q: %v", v, err)
		}
		o.command = s
	}
	if v, ok := values["args"]; ok {
		o.args = decodeQuotedList(t, v)
	}
	if v, ok := values["enabled_tools"]; ok {
		o.enabled = decodeQuotedList(t, v)
	}
	return o
}

// decodeQuotedList decodes a "[q1,q2,...]" value of individually
// strconv.Quote-d elements, exactly as internal/codex/mcp.go builds
// spec.Arguments() and spec.PermittedTools() into argv.
func decodeQuotedList(t *testing.T, raw string) []string {
	t.Helper()
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		t.Fatalf("not an encoded list: %q", raw)
	}
	body := raw[1 : len(raw)-1]
	var out []string
	for len(body) > 0 {
		prefix, prefixErr := strconv.QuotedPrefix(body)
		if prefixErr != nil || prefix == "" {
			t.Fatalf("malformed quoted element in %q: %v", raw, prefixErr)
		}
		s, err := strconv.Unquote(prefix)
		if err != nil {
			t.Fatalf("unquote %q: %v", prefix, err)
		}
		out = append(out, s)
		body = strings.TrimPrefix(body[len(prefix):], ",")
	}
	return out
}

// TestCodexMCPLifecycleEnabledDisabledAbsentSetup is AC-008's
// enabled/disabled/absent setup case, catalog parity and native-tool
// preservation, all through the real codex.Runner and the real broker.
//
//   - "enabled": the managed runtime is present and the request is a mode
//     with a checkpoint owner; prepareMCP runs, a real broker is started, and
//     the fake codex subprocess actually receives the -c overrides.
//   - "disabled": the runtime itself is nil (the daemon never composed
//     it) — registration is never attempted, and native argv is unaffected.
//   - "absent": the runtime is present and the provider supports MCP, but
//     this call is legacy/unmanaged (implement/validate with no checkpoint
//     session) and so never asks for registration at all — see
//     runMCPBound's own comment.
func TestCodexMCPLifecycleEnabledDisabledAbsentSetup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		disable bool
		mode    string
	}{
		{"enabled", false, "research"},
		{"disabled", true, "research"},
		{"absent", false, "implement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prep, _ := lifecycleMCPPreparation(t, tc.mode)
			home := prep.mcpRuntime.Paths.Home()
			if tc.disable {
				prep.mcpRuntime = nil
			}
			dumpPath := filepath.Join(t.TempDir(), "argv.dump")
			runner := realCodexRunner(t, home, codexArgvDumpScript(dumpPath))
			host := &codexHost{Runner: runner, before: func(ctx context.Context, spec *agent.MCPLaunchSpec) error {
				if spec == nil {
					return nil
				}
				if err := lifecycleMCPProbe(ctx, spec); err != nil {
					return err
				}
				// Registered ("enabled") cases go through shared finalization, which
				// requires a genuine submitted result; the fake subprocess never
				// produces one on its own terminal text.
				return codexLifecycleSubmit(ctx, spec, tc.mode)
			}}
			prep.runner = host
			req := codexLifecycleRequest(t, prep, tc.mode, 1)
			result, err := runSubmissionBound(t.Context(), prep, req)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !result.Succeeded() {
				t.Fatalf("codex did not succeed: %+v", result)
			}
			argv := readArgvDump(t, dumpPath)
			// Native tool ceiling/sandbox argv is unaffected in every case.
			for _, want := range []string{"exec", "--json", "--ignore-user-config", "--ignore-rules"} {
				found := false
				for _, a := range argv {
					if a == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("argv %v missing native flag %q", argv, want)
				}
			}
			if got, ok := argvFlagValue(argv, "--sandbox"); !ok || got != "workspace-write" {
				t.Errorf("argv %v sandbox = %q, want workspace-write", argv, got)
			}
			if argv[len(argv)-1] != "-" {
				t.Errorf("argv %v does not end with the stdin prompt argument", argv)
			}
			overrides := decodeCodexMCPOverrides(t, argv)
			if tc.name != "enabled" {
				if overrides != nil {
					t.Fatalf("%s registration reached argv: %+v", tc.name, overrides)
				}
				if host.spec != nil {
					t.Fatalf("%s host received a registration: %v", tc.name, host.spec)
				}
				return
			}
			if host.spec == nil {
				t.Fatal("enabled host never received registration")
			}
			if overrides == nil {
				t.Fatal("enabled registration did not reach argv")
			}
			if overrides.command != host.spec.Executable() {
				t.Errorf("command override = %q, want %q", overrides.command, host.spec.Executable())
			}
			if !slicesEqual(overrides.args, host.spec.Arguments()) {
				t.Errorf("args override = %v, want %v", overrides.args, host.spec.Arguments())
			}
			if overrides.env != "{}" {
				t.Errorf("env override = %q, want the empty object", overrides.env)
			}
			// Catalog parity: what the fake host actually received is exactly
			// the attempt's real installed catalog from the live broker —
			// spec.PermittedTools() is itself production-checked against
			// dispatcher.profile.List() inside prepareMCP, and lifecycleMCPProbe
			// (run from codexHost.before, before the subprocess even started)
			// independently confirmed the live broker answers with the same names.
			if !slicesEqual(overrides.enabled, host.spec.PermittedTools()) {
				t.Errorf("enabled_tools override = %v, want the live catalog %v", overrides.enabled, host.spec.PermittedTools())
			}
			lifecycleMCPRemoved(t, host.spec)
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// codexLifecycleSubmit performs a genuine communication_submit call through the
// attempt's live broker, exactly mirroring the reference success path in
// TestSharedCompletedResearchAndSpikeNativeTool (internal/turn/shared_finalization_test.go).
// The fake `codex` fixture is a shell script; it cannot itself speak MCP, so this
// drives the identical call a real provider would make. It must run before the
// fake subprocess exits, since spec.DescriptorPath() only answers while this
// attempt's broker (started by prepareMCP, ahead of Runner.Run) is still live.
func codexLifecycleSubmit(ctx context.Context, spec *agent.MCPLaunchSpec, mode string) error {
	var input agentresult.CreateInput
	if err := json.Unmarshal(resultCreate(mode), &input); err != nil {
		return err
	}
	input.Result.Outcome = "completed"
	input.Result.Reason = ""
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	client := submission.Client{DescriptorPath: spec.DescriptorPath()}
	response, err := client.Call(ctx, "communication_submit", raw)
	if err != nil {
		return err
	}
	if response.Error != "" {
		return fmt.Errorf("communication_submit: %s", response.Error)
	}
	var accepted AgentResultSubmission
	if err := json.Unmarshal(response.Result, &accepted); err != nil {
		return err
	}
	if accepted.Receipt == nil || accepted.Receipt.InputState != "accepted" {
		return fmt.Errorf("communication_submit rejected: %s", response.Result)
	}
	return nil
}

// TestCodexMCPLifecycleMalformedQuotedValuesSurvive is AC-008's
// malformed/quoted-values case: an LF_HOME (and therefore descriptor path)
// and a factory executable path carrying quote/backslash/apostrophe
// characters must reach the real codex subprocess's -c overrides exactly,
// and the real broker's own bookkeeping (registration, then clean teardown)
// must not be corrupted by them.
//
// Tool names cannot carry these characters: [agent.MCPLaunchSpec.Validate]
// restricts every catalog name to [a-z0-9_], so the values this test can
// legitimately stress are the paths that flow through the same encoding —
// which is exactly what an operator's own hostile-but-real home directory
// (an apostrophe in a username, for instance) would produce.
func TestCodexMCPLifecycleMalformedQuotedValuesSurvive(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t, "research")
	hostileHome := filepath.Join(launchFixtureDirectory(t), `l'i"te\backslash factory`)
	if err := os.MkdirAll(hostileHome, 0o700); err != nil {
		t.Fatal(err)
	}
	prep.mcpRuntime.Paths = config.NewPaths(hostileHome)
	prep.mcpRuntime.Executable = filepath.Join(hostileHome, "bin", `li"te'fac\tory`)

	dumpPath := filepath.Join(t.TempDir(), "argv.dump")
	runner := realCodexRunner(t, hostileHome, codexArgvDumpScript(dumpPath))
	// The broker's own bookkeeping (discovery, then a real tool call) survives
	// the hostile paths intact, checked while it is still live — prepareMCP's
	// broker is torn down once Run returns. The genuine communication_submit
	// call also proves shared finalization accepts a real success through the
	// hostile-path registration, matching the reference pattern in
	// TestSharedCompletedResearchAndSpikeNativeTool.
	host := &codexHost{Runner: runner, before: func(ctx context.Context, spec *agent.MCPLaunchSpec) error {
		if err := lifecycleMCPProbe(ctx, spec); err != nil {
			return err
		}
		return codexLifecycleSubmit(ctx, spec, "research")
	}}
	prep.runner = host

	req := codexLifecycleRequest(t, prep, "research", 1)
	result, err := runSubmissionBound(t.Context(), prep, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Succeeded() {
		t.Fatalf("codex did not succeed: %+v", result)
	}
	if host.spec == nil {
		t.Fatal("host never received registration")
	}
	if !strings.ContainsAny(host.spec.Executable(), `'"\`) {
		t.Fatalf("fixture executable %q does not actually carry hostile characters", host.spec.Executable())
	}
	if !strings.ContainsAny(host.spec.DescriptorPath(), `'"\`) {
		t.Fatalf("fixture descriptor %q does not actually carry hostile characters", host.spec.DescriptorPath())
	}

	argv := readArgvDump(t, dumpPath)
	overrides := decodeCodexMCPOverrides(t, argv)
	if overrides == nil {
		t.Fatal("registration did not reach argv")
	}
	if overrides.command != host.spec.Executable() {
		t.Errorf("command override = %q, want %q", overrides.command, host.spec.Executable())
	}
	if !slicesEqual(overrides.args, host.spec.Arguments()) {
		t.Errorf("args override = %v, want %v", overrides.args, host.spec.Arguments())
	}

	// Clean teardown after the run: the descriptor this attempt owned is gone.
	lifecycleMCPRemoved(t, host.spec)
}

// TestCodexMCPLifecycleServerCollisionRefusesSafely is AC-008's server
// collision/isolation case: an operator config.toml that already defines
// mcp_servers.litefactory must make the real codex.Runner refuse the launch
// (internal/codex's own checkMCPCollision), and that refusal must leave the
// real broker/authority cleanly closed rather than corrupted — a fresh
// attempt right after must still succeed.
func TestCodexMCPLifecycleServerCollisionRefusesSafely(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t, "research")
	home := prep.mcpRuntime.Paths.Home()

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"),
		[]byte("[mcp_servers.litefactory]\ncommand = \"attacker\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dumpPath := filepath.Join(t.TempDir(), "argv.dump")
	runner := realCodexRunner(t, home, codexArgvDumpScript(dumpPath), func(o *codex.Options) {
		o.Codex.AllowUserConfig = true // required for checkMCPCollision to read config.toml at all
		o.Environ = append(o.Environ, "CODEX_HOME="+codexHome)
	})
	var probed *agent.MCPLaunchSpec
	host := &codexHost{Runner: runner, before: func(ctx context.Context, spec *agent.MCPLaunchSpec) error {
		probed = spec
		// The broker is live and correct before this attempt is refused.
		return lifecycleMCPProbe(ctx, spec)
	}}
	prep.runner = host

	req := codexLifecycleRequest(t, prep, "research", 1)
	if _, err := runSubmissionBound(t.Context(), prep, req); err == nil {
		t.Fatal("collision with the reserved MCP server name was not refused")
	}
	if _, statErr := os.Stat(dumpPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("fake codex host launched despite the collision (argv dump exists): %v", statErr)
	}
	if probed == nil {
		t.Fatal("host never received registration to probe")
	}
	// Cleanup ran despite the refusal: the broker this attempt owned is gone.
	lifecycleMCPRemoved(t, probed)

	// The shared authority/database is unharmed: a fresh, non-colliding
	// attempt right after succeeds normally. It goes through shared
	// finalization exactly as the refused attempt would have, so it needs a
	// genuine communication_submit for a real success.
	dumpPath2 := filepath.Join(t.TempDir(), "argv.dump")
	prep.runner = &codexHost{Runner: realCodexRunner(t, home, codexArgvDumpScript(dumpPath2)), before: func(ctx context.Context, spec *agent.MCPLaunchSpec) error {
		if err := lifecycleMCPProbe(ctx, spec); err != nil {
			return err
		}
		return codexLifecycleSubmit(ctx, spec, "research")
	}}
	req2 := codexLifecycleRequest(t, prep, "research", 2)
	result, err := runSubmissionBound(t.Context(), prep, req2)
	if err != nil || !result.Succeeded() {
		t.Fatalf("authority/broker corrupted after collision refusal: result=%+v err=%v", result, err)
	}
}

// TestCodexMCPLifecycleRunPathsPreserveRegistration is AC-008's
// resume/fallback case: cold, resume and cold-after-resume-refused all carry
// their own independent registration end to end through the real broker,
// and codex's own resume subcommand shape is unaffected by the MCP overrides
// sitting alongside it in argv.
func TestCodexMCPLifecycleRunPathsPreserveRegistration(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t, "research")
	home := prep.mcpRuntime.Paths.Home()
	seenAttempts, seenPaths := map[string]bool{}, map[string]bool{}
	for i, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		t.Run(string(path), func(t *testing.T) {
			dumpPath := filepath.Join(t.TempDir(), "argv.dump")
			script := codexArgvDumpScript(dumpPath)
			if path == agent.PathResume {
				script = codexResumeRefusedScript(dumpPath)
			}
			host := &codexHost{Runner: realCodexRunner(t, home, script), before: lifecycleMCPProbe}
			prep.runner = host
			req := codexLifecycleRequest(t, prep, "research", i+1)
			req.Path = path
			if path == agent.PathResume {
				req.Session.Resume = "codex-resume-thread"
			}
			result, err := runSubmissionBound(t.Context(), prep, req)
			if path == agent.PathResume {
				if !errors.Is(err, agent.ErrSessionNotResumable) {
					t.Fatalf("resume refusal not preserved: result=%+v err=%v", result, err)
				}
			} else if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if host.spec == nil {
				t.Fatal("host never received registration")
			}
			if seenAttempts[host.spec.AttemptReference()] || seenPaths[host.spec.DescriptorPath()] {
				t.Fatal("this run path reused another's authority/resources")
			}
			seenAttempts[host.spec.AttemptReference()], seenPaths[host.spec.DescriptorPath()] = true, true

			argv := readArgvDump(t, dumpPath)
			if overrides := decodeCodexMCPOverrides(t, argv); overrides == nil {
				t.Fatalf("registration did not reach argv on %s: %v", path, argv)
			}
			if path == agent.PathResume {
				if got, ok := argvFlagValue(argv, "resume"); !ok || got != "codex-resume-thread" {
					t.Errorf("argv %v missing resume subcommand with the session's thread", argv)
				}
			}
			lifecycleMCPRemoved(t, host.spec)
		})
	}
}

// TestCodexMCPLifecycleConcurrentCancellationIsolation is AC-008's
// concurrent-attempts/cancellation/cleanup case: two real codex subprocesses
// launched concurrently under the one startup-owned authority never share a
// catalog or a descriptor, cancelling one leaves the other's broker
// reachable, and both are cleanly torn down.
func TestCodexMCPLifecycleConcurrentCancellationIsolation(t *testing.T) {
	first, db := lifecycleMCPPreparation(t, "research")
	second := *first
	repo, err := db.GetRepo(t.Context(), first.session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	second.session, err = db.GetOrCreateSession(t.Context(), repo, 275)
	if err != nil {
		t.Fatal(err)
	}
	second.ref, err = naming.ParseRef(second.session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	second.run.ID = agentResultRun(t, db, second.session, "research", "second-codex-run", 2).RunID
	home := first.mcpRuntime.Paths.Home()

	newFifoHost := func() (*codexHost, string) {
		worktree := t.TempDir()
		for _, name := range []string{"ready.fifo", "wait.fifo"} {
			if err := syscall.Mkfifo(filepath.Join(worktree, name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return &codexHost{Runner: realCodexRunner(t, home, codexReadyWaitScript)}, worktree
	}
	firstHost, firstWorktree := newFifoHost()
	secondHost, secondWorktree := newFifoHost()
	// Only the survivor needs a genuine submitted result: the cancelled attempt
	// is overridden to OutcomeAbandoned by shared finalization's own
	// cancellation handling regardless of application content, but the
	// survivor completes normally through shared finalization and this fake
	// subprocess (unlike codexArgvDumpScript) never emits any terminal text at
	// all, so it needs the real communication_submit for a real success.
	secondHost.before = func(ctx context.Context, spec *agent.MCPLaunchSpec) error {
		return codexLifecycleSubmit(ctx, spec, "research")
	}
	first.runner, second.runner = firstHost, secondHost

	req1 := codexLifecycleRequestIn(t, first, "research", firstWorktree, 1)
	req2 := codexLifecycleRequestIn(t, &second, "research", secondWorktree, 1)

	type response struct {
		result *agent.RunResult
		err    error
	}
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	secondCtx, cancelSecond := context.WithCancel(t.Context())
	defer cancelSecond()
	finished := make(chan response, 2)
	go func() {
		result, err := runSubmissionBound(firstCtx, first, req1)
		finished <- response{result, err}
	}()
	go func() {
		result, err := runSubmissionBound(secondCtx, &second, req2)
		finished <- response{result, err}
	}()

	awaitReady := func(worktree string) {
		t.Helper()
		// O_RDWR (not O_RDONLY) so opening this side never blocks waiting for a
		// writer, mirroring internal/harness/mcp_test.go's identical fixture.
		f, err := os.OpenFile(filepath.Join(worktree, "ready.fifo"), os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var b [1]byte
		if _, err := f.Read(b[:]); err != nil {
			t.Fatalf("fake codex host did not announce readiness: %v", err)
		}
	}
	awaitReady(firstWorktree)
	awaitReady(secondWorktree)

	if firstHost.spec == nil || secondHost.spec == nil {
		t.Fatal("both hosts must have received their registration by the time they signalled readiness")
	}
	if firstHost.spec.AttemptReference() == secondHost.spec.AttemptReference() ||
		firstHost.spec.DescriptorPath() == secondHost.spec.DescriptorPath() {
		t.Fatal("concurrent codex attempts share registration")
	}
	// Both live descriptors are independently reachable while both real
	// subprocesses are still blocked in flight.
	for _, spec := range []*agent.MCPLaunchSpec{firstHost.spec, secondHost.spec} {
		if err := lifecycleMCPProbe(t.Context(), spec); err != nil {
			t.Fatalf("in-flight broker unreachable: %v", err)
		}
	}

	cancelFirst()
	firstResponse := <-finished
	if firstResponse.err != nil {
		t.Fatalf("cancelled codex run returned an unexpected error: %v", firstResponse.err)
	}
	if firstResponse.result == nil || firstResponse.result.Outcome != agent.OutcomeAbandoned {
		t.Fatalf("cancelled result = %+v, want OutcomeAbandoned", firstResponse.result)
	}
	// Exactly the cancelled attempt's descriptor is gone; the survivor's
	// broker is untouched and still answers.
	lifecycleMCPRemoved(t, firstHost.spec)
	if err := lifecycleMCPProbe(t.Context(), secondHost.spec); err != nil {
		t.Fatalf("concurrent cancellation revoked the survivor: %v", err)
	}

	release, err := os.OpenFile(filepath.Join(secondWorktree, "wait.fifo"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := release.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := release.Close(); err != nil {
		t.Fatal(err)
	}
	secondResponse := <-finished
	if secondResponse.err != nil {
		t.Fatalf("surviving codex run failed: %v", secondResponse.err)
	}
	if secondResponse.result == nil || !secondResponse.result.Succeeded() {
		t.Fatalf("surviving codex run = %+v, want success", secondResponse.result)
	}
	lifecycleMCPRemoved(t, secondHost.spec)
}

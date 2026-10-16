package codex

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
)

func TestRunnerNormalizesCodexIntoSharedResult(t *testing.T) {
	runner, argv := fakeRunner(t, `
printf '%s\n' '{"type":"thread.started","thread_id":"thread-123"}'
printf '%s\n' '{"type":"turn.started"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}'
`)
	result, err := runner.Run(context.Background(), request(t, agent.PathCold))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Succeeded() || result.ResultText != "done" || result.ResumeToken != "thread-123" || !result.HasResult {
		t.Fatalf("result = %+v", result)
	}
	args := strings.Join(strings.Fields(string(mustRead(t, argv))), " ")
	for _, want := range []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox workspace-write", "-"} {
		if !strings.Contains(args, want) {
			t.Errorf("argv %q missing %q", args, want)
		}
	}
}

func TestProposeScheduleIsReadOnlyAndDoesNotAllocateARun(t *testing.T) {
	runner, argv := fakeRunner(t, `
printf '%s\n' '{"type":"thread.started","thread_id":"proposal-thread"}'
printf '%s\n' '{"type":"turn.started"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"{\"items\":[{\"payload\":\"/implement T-1\",\"due_at\":\"2026-09-05T01:00:00Z\"}]}"}}'
printf '%s\n' '{"type":"turn.completed"}'
`)
	proposal, err := runner.ProposeSchedule(t.Context(), agent.ScheduleProposalRequest{
		Description: "split the implementation", ReceivedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ProposeSchedule: %v", err)
	}
	if len(proposal.Items) != 1 || proposal.Items[0].Payload != "/implement T-1" {
		t.Fatalf("proposal = %+v", proposal)
	}
	got := strings.Join(strings.Fields(string(mustRead(t, argv))), " ")
	for _, want := range []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox read-only"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "--add-dir") || strings.Contains(got, "resume") {
		t.Errorf("proposal argv contains normal-run capability: %q", got)
	}
}

// The same flag that isolates a run from the operator's own config.toml also
// stops Codex reading the repository's .codex/skills, which is where this
// project's specialist instructions live. Opting in drops it; the default
// keeps it, and the case above already pins that default.
func TestAllowUserConfigDropsTheIsolationFlag(t *testing.T) {
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	runner.userConfig = true
	if _, err := runner.Run(context.Background(), request(t, agent.PathCold)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := strings.Join(strings.Fields(string(mustRead(t, argv))), " ")
	if strings.Contains(got, "--ignore-user-config") {
		t.Errorf("argv %q still isolates the run from its own config", got)
	}
	for _, want := range []string{"exec", "--json", "--ignore-rules", "--sandbox workspace-write"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q missing %q", got, want)
		}
	}
	if !strings.HasSuffix(got, "-") {
		t.Errorf("argv %q does not end with the stdin prompt argument", got)
	}
}

func TestRunnerResumesExplicitThreadAndReadsLongLines(t *testing.T) {
	runner, argv := fakeRunner(t, `
printf '{"type":"thread.started","thread_id":"thread-123","padding":"'
head -c 70000 /dev/zero | tr '\000' x
printf '%s\n' '"}'
printf '%s\n' '{"type":"turn.started"}' '{"type":"turn.completed"}'
`)
	req := request(t, agent.PathResume)
	req.Session.Resume = "thread-123"
	result, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.LongestLine < 70000 || result.Malformed != 0 {
		t.Fatalf("result = %+v", result)
	}
	if got := string(mustRead(t, argv)); !strings.Contains(got, "resume thread-123 -") {
		t.Fatalf("argv = %q", got)
	}
}

// A workspace-write sandbox does not cover a toolchain cache living in the
// operator's home directory: measured against codex-cli 0.152.1, `go build`
// inside one fails with "operation not permitted" on $GOCACHE. Each
// configured writable root must therefore reach argv as its own --add-dir,
// and a relative one must be refused at construction rather than resolved
// against the worktree.
func TestWritableRootsBecomeAddDirAndMustBeAbsolute(t *testing.T) {
	dir := t.TempDir()
	cache, mod := filepath.Join(dir, "go-build"), filepath.Join(dir, "pkg", "mod")
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`, cache, mod)
	if _, err := runner.Run(context.Background(), request(t, agent.PathCold)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := string(mustRead(t, argv))
	for _, want := range []string{"--add-dir " + cache, "--add-dir " + mod} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q missing %q", got, want)
		}
	}
	// The prompt argument stays last, so the roots cannot displace it.
	if !strings.HasSuffix(strings.TrimSpace(got), "-") {
		t.Errorf("argv %q does not end with the stdin prompt argument", got)
	}

	if _, err := newRunner(t, t.TempDir(), "relative/cache"); err == nil {
		t.Error("a relative writable root was accepted; want a construction error")
	}
}

// No directory grant reaches the sandbox's network ban, and the ban is what
// stops net/http/httptest binding a loopback port — the failure that ended
// issue #32's Implement run. Off by default, and one -c override when the
// operator opts in.
func TestNetworkAccessIsOptInAndReachesArgv(t *testing.T) {
	const override = "-c sandbox_workspace_write.network_access=true"

	off, offArgv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	if _, err := off.Run(context.Background(), request(t, agent.PathCold)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := string(mustRead(t, offArgv)); strings.Contains(got, "network_access") {
		t.Errorf("argv %q carries a network override with the key unset", got)
	}

	on, onArgv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	on.network = true
	if _, err := on.Run(context.Background(), request(t, agent.PathCold)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := strings.Join(strings.Fields(string(mustRead(t, onArgv))), " ")
	if !strings.Contains(got, override) {
		t.Errorf("argv %q missing %q", got, override)
	}
	if !strings.HasSuffix(got, "-") {
		t.Errorf("argv %q does not end with the stdin prompt argument", got)
	}
}

func fakeRunner(t *testing.T, body string, writable ...string) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	argv := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s ' \"$@\" > \"" + argv + "\"\n" + body
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := newRunner(t, dir, writable...)
	if err != nil {
		t.Fatal(err)
	}
	return r, argv
}

// newRunner builds a Runner over the fake binary in dir and returns the
// construction error rather than failing, so a test can assert on a refusal.
func newRunner(t *testing.T, dir string, writable ...string) (*Runner, error) {
	t.Helper()
	binary := filepath.Join(dir, "codex")
	if _, err := os.Stat(binary); err != nil {
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return New(Options{
		Paths:   config.NewPaths(filepath.Join(dir, "lfhome")),
		Codex:   config.Codex{Binary: binary, TimeoutSeconds: 30, WritableRoots: writable},
		Model:   "gpt-test",
		Environ: []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "USER=test"},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func request(t *testing.T, path agent.RunPath) agent.Request {
	t.Helper()
	worktree := t.TempDir()
	return agent.Request{Ref: naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 1}, RunOrdinal: 1,
		SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "local-ref"}, Mode: "infer", Path: path,
		WorktreePath: worktree, Prompt: "do the task",
		Permissions: agent.Permissions{Profile: "implement", Payload: Sandbox{Mode: "workspace-write"}}}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A typed contribution is handed back through a broker directory under LF_HOME,
// outside the worktree a workspace-write run may write to. Codex rejects the
// helper's journal write with "writing outside of the project" unless the
// directory reaches argv, which on issue #208 turned a completed ten-minute
// Plan into a run that recorded nothing.
func TestSubmissionRootBecomesAddDirAndMustBeAbsolute(t *testing.T) {
	broker := filepath.Join(t.TempDir(), "s", "d5868bf912d89279")
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	req := request(t, agent.PathCold)
	req.SubmissionRoot = broker
	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := string(mustRead(t, argv))
	if !strings.Contains(got, "--add-dir "+broker) {
		t.Errorf("argv %q missing the broker directory", got)
	}
	// The prompt argument stays last, as for the configured roots.
	if !strings.HasSuffix(strings.TrimSpace(got), "-") {
		t.Errorf("argv %q does not end with the stdin prompt argument", got)
	}

	// A run that submits nothing opens nothing.
	runner, argv = fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	if _, err := runner.Run(context.Background(), request(t, agent.PathCold)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := string(mustRead(t, argv)); strings.Contains(got, "--add-dir") {
		t.Errorf("argv %q opened a directory for a run with no submission", got)
	}

	// A relative root is a caller bug and must not be resolved against the
	// worktree, where it would silently widen a different directory.
	bad := request(t, agent.PathCold)
	bad.SubmissionRoot = "s/d5868bf912d89279"
	if err := bad.Validate(); err == nil {
		t.Error("a relative submission root was accepted; want a validation error")
	}
}

func TestRunnerRecoversMissingExecutable(t *testing.T) {
	runner, _ := fakeRunner(t, `printf '%s\n' '{"type":"thread.started","thread_id":"thread-123"}' '{"type":"turn.completed"}'`)
	binary := runner.binary.Path
	if err := os.Rename(binary, binary+".saved"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { time.Sleep(time.Second); done <- os.Rename(binary+".saved", binary) }()
	result, err := runner.Run(t.Context(), request(t, agent.PathCold))
	if restoreErr := <-done; restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err != nil || result == nil || !result.Succeeded() {
		t.Fatalf("recovery: %+v %v", result, err)
	}
}

func TestRunnerRecoveryRechecksDelivery(t *testing.T) {
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	req := request(t, agent.PathCold)
	req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "research", MainModel: "worker", Roles: []string{"research"}}
	instruction := filepath.Join(req.Delivery.Root, "main-instructions.md")
	if err := os.WriteFile(instruction, []byte("Selected role"), 0600); err != nil {
		t.Fatal(err)
	}
	manifestDelivery(t, req.Delivery)
	binary := runner.binary.Path
	if err := os.Rename(binary, binary+".saved"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(time.Second)
		if err := os.WriteFile(instruction, []byte("changed"), 0600); err != nil {
			done <- err
			return
		}
		done <- os.Rename(binary+".saved", binary)
	}()
	result, err := runner.Run(t.Context(), req)
	if restoreErr := <-done; restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if !errors.Is(err, agent.ErrDeliveryRefused) || result != nil {
		t.Fatalf("changed delivery launched: %+v %v", result, err)
	}
	if _, err = os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("process launched: %v", err)
	}
}

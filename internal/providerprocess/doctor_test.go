package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

func TestDoctorProcessCancellationAndNativeFailure(t *testing.T) {
	for _, test := range []struct {
		name, body string
		cancel     bool
	}{{"timeout", "/bin/sleep 20", true}, {"failure", "exit 7", false}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "fixture")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+test.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "context", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credential-fixture"}}}
			err = RunDoctorProcess(t.Context(), binary, nil, execution.IsolatedEnvironment(nil, dir), request, 50*time.Millisecond, func([]byte) {})
			if err == nil {
				t.Fatal("expected failure")
			}
			if test.cancel && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout identity lost: %v", err)
			}
			if strings.Contains(err.Error(), binary) {
				t.Fatalf("native path disclosed: %v", err)
			}
		})
	}
}

func TestDoctorProcessUsesAdapterOwnedCancellation(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "provider")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\n/bin/sleep 20\n"), 0700); err != nil {
		t.Fatal(err)
	}
	spec, err := agent.NewMCPLaunchSpec(fixture, filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "inspect", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credentials"}}}
	cancelled := make(chan int, 1)
	err = RunDoctorProcess(t.Context(), fixture, nil, execution.IsolatedEnvironment(nil, dir), request, 50*time.Millisecond, func([]byte) {}, func(pid int) error {
		cancelled <- pid
		return syscall.Kill(-pid, syscall.SIGKILL)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	select {
	case pid := <-cancelled:
		if pid <= 1 || pid == os.Getpid() {
			t.Fatalf("wrong cancellation identity %d", pid)
		}
	default:
		t.Fatal("adapter subtree cancellation was not called")
	}
}

// Claude files its keychain login under USER; an isolated run without it uses
// a separate "unknown" login that nothing else refreshes.
func TestJudgeProcessSkipsOversizedOutput(t *testing.T) {
	const huge = 1024*1024 + 4096
	const normal = 1000000
	for _, test := range []struct {
		name, body string
		kind       string
		largest    int
		total      int
		observed   int
	}{
		{"line dropped", "printf '{\"type\":\"start\"}\\n'\n" + xLine(huge) + strings.Repeat(xLine(normal), 9) + "printf '{\"type\":\"done\"}\\n'\n", "line_dropped", huge, 17 + huge + 1 + 9*(normal+1) + 16, 11},
		{"total exceeded", strings.Repeat(xLine(normal), 9) + "printf '{\"type\":\"done\"}\\n'\n", "total_exceeded", 0, 9*(normal+1) + 16, 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, binary := writeOutputFixture(t, test.body)
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.JudgeToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "judge", WorkingDirectory: dir, MCPLaunch: spec}}
			var lines []string
			type report struct {
				kind           string
				largest, total int
			}
			var reports []report
			err = RunJudgeProcess(t.Context(), binary, nil, execution.IsolatedEnvironment([]string{"PATH=/usr/bin:/bin"}, dir), request, 20*time.Second, func(line []byte) {
				if len(line) > 1024*1024 {
					t.Errorf("oversized line observed: %d bytes", len(line))
				}
				lines = append(lines, string(line[:min(len(line), 32)]))
			}, func(kind string, largest, total int) { reports = append(reports, report{kind, largest, total}) })
			if err != nil {
				t.Fatalf("judge ended by output guard: %v", err)
			}
			if len(lines) != test.observed || lines[len(lines)-1] != `{"type":"done"}` {
				t.Fatalf("observed %d lines, last %q", len(lines), lines[len(lines)-1])
			}
			if len(reports) != 1 || reports[0] != (report{test.kind, test.largest, test.total}) {
				t.Fatalf("oversized reports: %+v", reports)
			}
		})
	}
}

func TestJudgeProcessNormalOutputReportsNothing(t *testing.T) {
	dir, binary := writeOutputFixture(t, "printf '{\"type\":\"done\"}\\n'")
	spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "judge", WorkingDirectory: dir, MCPLaunch: spec}}
	calls := 0
	if err := RunJudgeProcess(t.Context(), binary, nil, execution.IsolatedEnvironment(nil, dir), request, 10*time.Second, func([]byte) {}, func(string, int, int) { calls++ }); err != nil || calls != 0 {
		t.Fatalf("normal judge output: %v, %d reports", err, calls)
	}
}

func TestNonJudgeOutputGuardReportsItself(t *testing.T) {
	body := xLine(1024*1024+1) + "/bin/sleep 20"
	for _, test := range []struct {
		name string
		run  func(t *testing.T, dir, binary string) error
	}{
		{"doctor", func(t *testing.T, dir, binary string) error {
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "context", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credential-fixture"}}}
			return RunDoctorProcess(t.Context(), binary, nil, execution.IsolatedEnvironment([]string{"PATH=/usr/bin:/bin"}, dir), request, 10*time.Second, func([]byte) {})
		}},
		{"repair", func(t *testing.T, dir, binary string) error {
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.RepairToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.RepairRequest{Operation: "operation", Invocation: agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "context", WorkingDirectory: dir, MCPLaunch: spec}}
			return RunRepairProcess(t.Context(), binary, nil, execution.IsolatedEnvironment([]string{"PATH=/usr/bin:/bin"}, dir), request, 10*time.Second, func([]byte) {})
		}},
		{"chat", func(t *testing.T, dir, binary string) error {
			request := agent.ChatRequest{Model: "configured", Prompt: "context", WorkingDirectory: dir}
			return RunChatProcess(t.Context(), binary, nil, execution.IsolatedEnvironment([]string{"PATH=/usr/bin:/bin"}, dir), request, 10*time.Second, func([]byte) {})
		}},
		{"drift", func(t *testing.T, dir, binary string) error {
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.DriftToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.DriftRequest{Invocation: agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "context", WorkingDirectory: dir, MCPLaunch: spec}}
			return RunDriftProcess(t.Context(), binary, nil, execution.IsolatedEnvironment([]string{"PATH=/usr/bin:/bin"}, dir), request, 10*time.Second, func([]byte) {})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, binary := writeOutputFixture(t, body)
			err := test.run(t, dir, binary)
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "output line budget") {
				t.Fatalf("guard error hidden: %v", err)
			}
		})
	}
}

func writeOutputFixture(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ncat >/dev/null\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir, binary
}

// xLine prints one line of n bytes of 'x' from the fixture shell.
func xLine(n int) string {
	return "head -c " + strconv.Itoa(n) + " /dev/zero | tr '\\0' x; printf '\\n'\n"
}

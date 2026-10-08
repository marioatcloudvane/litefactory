package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestDoctorOutputBoundsAndFragments(t *testing.T) {
	var lines []string
	output := doctorLines{observe: func(line []byte) { lines = append(lines, string(line)) }}
	for _, part := range []string{"{\"type\":", "\"done\"}\n{}\n"} {
		if _, err := output.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := output.finish(); err != nil || len(lines) != 2 || lines[0] != "{\"type\":\"done\"}" {
		t.Fatalf("fragments: %v %v", lines, err)
	}
	if _, err := output.Write([]byte(strings.Repeat("x", 1024*1024+1))); err == nil {
		t.Fatal("unbounded record accepted")
	}
	partial := doctorLines{observe: func([]byte) {}}
	if _, err := partial.Write([]byte("{\"incomplete\":")); err != nil {
		t.Fatal(err)
	}
	if partial.finish() == nil {
		t.Fatal("partial terminal record accepted")
	}
	total := doctorLines{total: 8 * 1024 * 1024, observe: func([]byte) {}}
	if _, err := total.Write([]byte("\n")); err == nil {
		t.Fatal("total output bound ignored")
	}
}

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
			err = RunDoctorProcess(t.Context(), binary, nil, DoctorEnvironment(nil, dir), request, 50*time.Millisecond, func([]byte) {})
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
	err = RunDoctorProcess(t.Context(), fixture, nil, DoctorEnvironment(nil, dir), request, 50*time.Millisecond, func([]byte) {}, func(pid int) error {
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

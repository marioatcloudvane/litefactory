package codex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSelectedCleanupNormalExit(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			dir := t.TempDir()
			pidfile := filepath.Join(dir, "child")
			var output bytes.Buffer
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			capture, err := Execute(ctx, Prepared{Binary: "/bin/sh", Arguments: []string{"-c", `sleep 30 & child=$!; printf '%s' "$child" > "$1"; printf 'retained output'; exit "$2"`, "native", pidfile, strconv.Itoa(code)}, Environment: []string{"PATH=/bin:/usr/bin"}, Directory: dir, CleanupTree: true, Consume: func(r io.Reader) { _, _ = io.Copy(&output, r) }})
			if err != nil || capture.ExitCode != code || output.String() != "retained output" {
				t.Fatalf("capture=%+v error=%v output=%q", capture, err, output.String())
			}
			if code == 0 && capture.WaitError != nil {
				t.Fatalf("success changed: %v", capture.WaitError)
			}
			pid := readSelectedPID(t, pidfile)
			assertSelectedStopped(t, pid)
		})
	}
}

func TestSelectedCleanupPreservesOutput(t *testing.T) {
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	capture, err := Execute(ctx, Prepared{Binary: "/bin/sh", Arguments: []string{"-c", `i=0; while [ "$i" -lt 10000 ]; do printf 'output-line\n'; i=$((i + 1)); done`}, Directory: t.TempDir(), CleanupTree: true, Consume: func(r io.Reader) { _, _ = io.Copy(&output, r) }})
	if err != nil || capture.WaitError != nil || output.String() != strings.Repeat("output-line\n", 10000) {
		t.Fatalf("output truncated: %d bytes; capture=%+v, %v", output.Len(), capture, err)
	}
}

func TestSelectedCleanupDetachedCancellation(t *testing.T) {
	probe := exec.CommandContext(t.Context(), "/usr/bin/pgrep", "-P", strconv.Itoa(os.Getpid()))
	if output, err := probe.CombinedOutput(); err != nil && len(output) > 0 {
		t.Skipf("existing subtree enumeration unavailable: %s", output)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan Capture, 1)
	go func() {
		capture, _ := Execute(ctx, Prepared{Binary: executable, Arguments: []string{"-test.run=^TestSelectedCleanupHelper$"}, Environment: append(os.Environ(), "LF_SELECTED_HELPER=parent", "LF_SELECTED_PID="+pidfile), Directory: dir, CleanupTree: true})
		done <- capture
	}()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(pidfile); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("child did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	pid := readSelectedPID(t, pidfile)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	assertSelectedStopped(t, pid)
}

func TestSelectedCleanupHelper(t *testing.T) {
	switch os.Getenv("LF_SELECTED_HELPER") {
	case "child":
		time.Sleep(30 * time.Second)
	case "parent":
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(executable, "-test.run=^TestSelectedCleanupHelper$")
		cmd.Env = append(os.Environ(), "LF_SELECTED_HELPER=child")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("LF_SELECTED_PID"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
	}
}

func readSelectedPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func assertSelectedStopped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// This PID is explicitly recorded from the child started by this test.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("owned child %d survived cleanup", pid)
}

func TestSelectedCleanupAdmissionClosesPipes(t *testing.T) {
	before, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip("descriptor inventory unavailable")
	}
	refused := errors.New("admission refused")
	for range 16 {
		capture, err := Execute(t.Context(), Prepared{Binary: "/bin/sh", Directory: t.TempDir(), CleanupTree: true, BeforeLaunch: func(context.Context) error { return refused }})
		if !errors.Is(err, refused) || capture.Started {
			t.Fatalf("unexpected launch: %+v, %v", capture, err)
		}
	}
	after, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before)+2 {
		t.Fatalf("admission leaked descriptors: before=%d after=%d", len(before), len(after))
	}
}

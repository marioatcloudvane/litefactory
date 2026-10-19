package providerprocess

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The subtree walk is the mechanism SD-1 requires. These tests exercise it
// directly; the harness and codex runner tests exercise it end to end.

func TestDescendantsWalksMoreThanOneLevel(t *testing.T) {
	requireProcessListAccess(t)

	// A shell that starts a shell that starts a sleeper: three levels, so a
	// walk that only listed direct children would miss the bottom one.
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "/bin/sh -c '/bin/sleep 30 & wait' & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = KillSubtree(context.WithoutCancel(t.Context()), pid, discardLogger())
		_ = cmd.Wait()
	})

	var found []int
	if !waitFor(10*time.Second, func() bool {
		found = descendants(t.Context(), pid, discardLogger())
		return len(found) >= 2
	}) {
		t.Fatalf("descendants found %v; expected at least the intermediate shell and the sleeper", found)
	}
	if slices.Contains(found, pid) {
		t.Error("the walk returned the root itself as a descendant")
	}
	for _, child := range found {
		if child <= 1 {
			t.Errorf("the walk returned pid %d", child)
		}
	}
}

// requireProcessListAccess skips process-tree assertions only in the macOS
// sandbox that explicitly refuses pgrep's process-list query. Normal pgrep
// exit status 1 means the selected parent simply has no children, so it must
// not turn an environmental failure into a skip.
func requireProcessListAccess(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}

	cmd := exec.CommandContext(t.Context(), pgrepPath, "-P", strconv.Itoa(os.Getpid()))
	out, err := cmd.CombinedOutput()
	if strings.Contains(string(out), "pgrep: Cannot get process list") {
		t.Skip("macOS sandbox denies process-list access required by process-tree test")
	}
	if err != nil && !isExitCode(err, 1) {
		t.Logf("process-list preflight returned %v without an explicit sandbox denial", err)
	}
}

func TestProcessGroupsIncludesTheRoot(t *testing.T) {
	self, err := syscall.Getpgid(0)
	if err != nil {
		t.Fatalf("getpgid: %v", err)
	}
	groups := processGroups(t.Context(), os.Getpid(), discardLogger())
	if !slices.Contains(groups, self) {
		t.Fatalf("process groups %v does not contain our own %d", groups, self)
	}
}

// A kill(-pgid) aimed at the daemon's own group would take down the daemon —
// and, under `go test`, this binary. The guarantee is checked rather than
// assumed, so this test surviving is the assertion.
func TestSignalGroupsNeverSignalsItselfOrInit(t *testing.T) {
	self, err := syscall.Getpgid(0)
	if err != nil {
		t.Fatalf("getpgid: %v", err)
	}
	signalGroups([]int{self, 0, 1, -5}, syscall.SIGKILL, self, discardLogger())
	t.Log("still alive: the own-group, init and unspecified-group guards hold")
}

func TestKillSubtreeIgnoresImpossiblePIDs(t *testing.T) {
	for _, pid := range []int{0, 1, -1} {
		if err := KillSubtree(t.Context(), pid, discardLogger()); err != nil {
			t.Errorf("KillSubtree(%d) = %v, want nil", pid, err)
		}
	}
}

// KillSubtree runs as cmd.Cancel, where a returned error displaces the exit
// status the caller needs. It reports nil and logs instead — including for a
// process that has already gone.
func TestKillSubtreeIsBestEffort(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "/usr/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := KillSubtree(t.Context(), pid, discardLogger()); err != nil {
		t.Fatalf("KillSubtree on an exited process = %v, want nil", err)
	}
}

func TestChildrenReportsNothingForALeaf(t *testing.T) {
	if got := children(t.Context(), os.Getpid(), discardLogger()); len(got) != 0 {
		// The test binary may legitimately have children if something else in
		// the package left one behind; that is worth knowing about.
		t.Logf("the test process has children %v", got)
	}
	// A pid that cannot exist: pgrep exits 1, which is an ordinary answer and
	// not an error worth propagating.
	if got := children(t.Context(), 2_000_000_000, discardLogger()); len(got) != 0 {
		t.Errorf("children of a nonexistent pid = %v", got)
	}
}

func TestIsExitCode(t *testing.T) {
	err := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exit 1").Run()
	if !isExitCode(err, 1) {
		t.Errorf("isExitCode(%v, 1) = false", err)
	}
	if isExitCode(err, 2) {
		t.Errorf("isExitCode(%v, 2) = true", err)
	}
	if isExitCode(nil, 1) {
		t.Error("isExitCode(nil, 1) = true")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitFor polls until cond is true or the budget runs out.
func waitFor(budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

package harness

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The subtree walk itself (SD-1) is tested in internal/providerprocess;
// TestRunTimeoutKillsTheWholeSubtree exercises it end to end through the
// harness.

// pgrepPath is the process-tree enumerator the shared sweep uses.
const pgrepPath = "/usr/bin/pgrep"

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
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		t.Logf("process-list preflight returned %v without an explicit sandbox denial", err)
	}
}

func TestKillSubtreeIgnoresImpossiblePIDs(t *testing.T) {
	for _, pid := range []int{0, 1, -1} {
		if err := killSubtree(t.Context(), pid, discardLogger()); err != nil {
			t.Errorf("killSubtree(%d) = %v, want nil", pid, err)
		}
	}
}

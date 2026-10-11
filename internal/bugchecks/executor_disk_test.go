package bugchecks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestCheckRefusedBelowFreeDiskThreshold(t *testing.T) {
	isolation, worktree, _ := stackCommandFixture(t, "touch \"$0.started\"\nexit 0\n")
	isolation.MinFreeBytes = 15 << 30
	isolation.FreeBytes = func(string) (uint64, error) { return 1 << 30, nil }
	launches := 0
	got := isolation.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/echo", "test"}, TimeoutSeconds: 10}, func(CommandResult) error { launches++; return nil })
	if got.Outcome != "blocked" || !strings.Contains(got.Reason, "insufficient free disk space") {
		t.Fatalf("low disk classified %q %q", got.Outcome, got.Reason)
	}
	if launches != 0 || !got.Started.IsZero() {
		t.Fatal("check launched below the free disk threshold")
	}
	if _, err := os.Stat(isolation.Binary + ".started"); !os.IsNotExist(err) {
		t.Fatalf("launcher ran below the free disk threshold: %v", err)
	}
}

func TestCheckRunsAboveFreeDiskThreshold(t *testing.T) {
	isolation, worktree, _ := stackCommandFixture(t, "exit 0\n")
	isolation.MinFreeBytes = 15 << 30
	isolation.FreeBytes = func(string) (uint64, error) { return 20 << 30, nil }
	got := isolation.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/echo", "test"}, TimeoutSeconds: 10}, func(CommandResult) error { return nil })
	if got.Outcome == "blocked" && strings.Contains(got.Reason, "retain command identity: operation not permitted") {
		t.Skip("host refuses process identity observation required by isolated command executor")
	}
	if got.Outcome != "passed" {
		t.Fatalf("check above threshold classified %q %q", got.Outcome, got.Reason)
	}
}

func TestFreeDiskBytesReadsTheVolume(t *testing.T) {
	free, err := FreeDiskBytes(t.TempDir())
	if err != nil || free == 0 {
		t.Fatalf("FreeDiskBytes = %d, %v", free, err)
	}
	if _, err := FreeDiskBytes(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing path read as a volume")
	}
}

func TestCheckNoSpaceLeftIsInfrastructure(t *testing.T) {
	for _, tc := range []struct {
		name, body, outcome, reason string
	}{
		{"enospc", "echo 'TempDir: mkdir /tmp/x: no space left on device'\nexit 1\n", "blocked", "check infrastructure failure: no space left on device"},
		{"enospc_upper", "echo 'write: No Space Left On Device'\nexit 2\n", "blocked", "check infrastructure failure: no space left on device"},
		{"ordinary", "echo 'FAIL: TestSomething'\nexit 1\n", "failed", "project check reported test failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolation, worktree, _ := stackCommandFixture(t, tc.body)
			got := isolation.Run(t.Context(), worktree, config.BugCheckCommand{Argv: []string{"/bin/echo", "test"}, TimeoutSeconds: 10}, func(CommandResult) error { return nil })
			if got.Outcome == "blocked" && strings.Contains(got.Reason, "retain command identity: operation not permitted") {
				t.Skip("host refuses process identity observation required by isolated command executor")
			}
			if got.Outcome != tc.outcome || got.Reason != tc.reason {
				t.Fatalf("classified %q %q, want %q %q", got.Outcome, got.Reason, tc.outcome, tc.reason)
			}
		})
	}
}

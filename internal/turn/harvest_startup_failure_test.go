package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestGenuineStartupFailureReportsAgentNeverStarted is issue #311 (AC-005)'s
// contrast case: when the subprocess truly never produced a [harness.RunResult]
// — [runAndHarvest]'s runResult == nil branch — the report must still say the
// agent did not start, exactly as before the fix. The fix in #311 only
// changes what happens when a RunResult *was* produced (see
// TestHarvestStoppedFactoryFailureMatrix); this genuine startup failure must
// keep reporting the same truthful cause.
func TestGenuineStartupFailureReportsAgentNeverStarted(t *testing.T) {
	launchErr := errors.New("fork/exec /usr/local/bin/claude: permission denied")
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, launchErr
	})
	f := newFixture(t, runner)

	result := f.execute(f.request())

	if result.Succeeded() {
		t.Fatalf("a genuine startup failure was reported as a success: %+v", result)
	}
	if !strings.Contains(result.Reason, "coding agent did not start") {
		t.Fatalf("Reason = %q, want it to say the agent did not start", result.Reason)
	}
	if !strings.Contains(result.Reason, launchErr.Error()) {
		t.Fatalf("Reason = %q, lost the original launch error %q", result.Reason, launchErr.Error())
	}
	if result.Run.Status != store.RunFailed {
		t.Fatalf("run status = %q, want %q", result.Run.Status, store.RunFailed)
	}
}

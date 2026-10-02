package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestReportOfARunThatFailedBeforeAnAgentNamesTheRecordedError is #429's
// misreport. The graph Plan save step starts its run row and then failed with
// "plan brief Story Index missing" before any coding agent launched. Execute
// returned that error, the daemon left the comment unconsumed, and the next
// tick replayed the failed run and reported it as "outcome unknown — most
// likely interrupted". The report now names the recorded error and says the
// turn did not run.
func TestReportOfARunThatFailedBeforeAnAgentNamesTheRecordedError(t *testing.T) {
	t.Parallel()

	const cause = "plan brief Story Index missing"
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("a coding agent ran for a comment that had already produced a run")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	// The run row exactly as finalize leaves it: started, then finished failed
	// with only the recorded error — no exit code, no harness result.
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, storedClaudeSessionID, store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{
		SessionID:        f.session.ID,
		SegmentID:        segment.ID,
		Mode:             string(agent.ModePlan),
		Agent:            store.AgentClaude,
		TriggerCommentID: testComment,
		LogPath:          func(int) string { return filepath.Join(f.paths.Home(), "logs", "seeded.jsonl") },
	})
	mustProgress(t, err)
	// AllocateRun already yields a started (running) row here, as the
	// existing seedFinishedRun relies on; finish it the way finalize does.
	_, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, Error: cause})
	mustProgress(t, err)

	// The retry tick: the same comment replays the failed run and reports it.
	result := f.execute(f.request())
	if result.Disposition != DispositionAlreadyRan || !result.Reportable() {
		t.Fatalf("replay = %s reportable=%t, want an unreported failed run", result.Disposition, result.Reportable())
	}
	if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}
	post := forge.only(t)
	if !strings.Contains(post.body, cause) {
		t.Fatalf("report does not name the recorded error %q:\n%s", cause, post.body)
	}
	if !strings.Contains(post.body, "did not run") {
		t.Fatalf("report does not say the turn did not run:\n%s", post.body)
	}
	for _, banned := range []string{"outcome unknown", "most likely interrupted"} {
		if strings.Contains(post.body, banned) {
			t.Fatalf("report still says %q:\n%s", banned, post.body)
		}
	}
	if runner.callCount() != 0 {
		t.Fatalf("a coding agent was invoked %d times on the replay, want 0", runner.callCount())
	}
}

// TestTurnFailureLeavesRunsWithAnAgentExitAlone keeps #429's change to runs
// that failed before any agent exited: a replayed run with an exit code keeps
// its existing report input.
func TestTurnFailureLeavesRunsWithAnAgentExitAlone(t *testing.T) {
	t.Parallel()

	exitCode := int64(store.SignalExitCode)
	exited := Result{Disposition: DispositionAlreadyRan, Run: store.Run{Status: store.RunFailed, ExitCode: &exitCode, Error: store.ReasonAbandoned}}
	if err := turnFailure(exited); err != nil {
		t.Fatalf("turnFailure for a run with an agent exit = %v, want nil", err)
	}
	started := Result{Disposition: DispositionAlreadyRan, Run: store.Run{Status: store.RunFailed, Error: "plan brief Story Index missing"}}
	if err := turnFailure(started); err == nil || err.Error() != "plan brief Story Index missing" {
		t.Fatalf("turnFailure for a run that failed before an agent = %v, want the recorded error", err)
	}
	own := errors.New("push failed")
	if err := turnFailure(Result{Err: own, Run: started.Run}); !errors.Is(err, own) {
		t.Fatalf("turnFailure = %v, want the turn's own error first", err)
	}
}

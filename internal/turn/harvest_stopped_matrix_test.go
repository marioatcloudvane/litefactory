package turn

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// newHarvestStoppedPrep allocates one real run and returns the minimal
// [preparation] harvestStopped needs. prep.promptInput.Validation is set to
// a non-nil, harmless value so [save] takes its existing early-return path
// exactly the way a Validate-mode turn does — this test's concern is
// harvestStopped's factory-failure attribution, not [commitAndPush]'s git
// plumbing, and reusing the codebase's own existing short-circuit is more
// honest than a new special case. Mode is deliberately not "plan", so
// publishPlanHandoff and markManagedPlanReady take their own pre-existing
// no-op paths (AC-006 is exercised separately, through the full Execute()
// pipeline that every other successful-run test already goes through).
func newHarvestStoppedPrep(t *testing.T, f *fixture) (*preparation, store.Run) {
	t.Helper()
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "harvest-stopped", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	prep := &preparation{
		ref:         f.ref(),
		repo:        f.storeRepo,
		session:     f.session,
		segment:     seg,
		run:         run,
		request:     agent.Request{Ref: f.ref(), RunOrdinal: run.Ordinal},
		promptInput: harness.PromptInput{Validation: &agent.ValidationContext{}},
	}
	return prep, run
}

func runResultAt(outcome agent.Outcome, reason string, exitCode int) *agent.RunResult {
	now := time.Now()
	return &agent.RunResult{Outcome: outcome, Reason: reason, ExitCode: exitCode, HasResult: outcome == agent.OutcomeSucceeded, ResultText: "agent produced output", StartedAt: now.Add(-time.Second), FinishedAt: now, Duration: time.Second}
}

// TestHarvestStoppedFactoryFailureMatrix is issue #311's AC-005: every
// combination of the subprocess's own outcome and a local factory fault
// discovered only afterward must retain the process's truthful outcome
// (exit code, log path, result text via result.Harness/result.Outcome),
// attribute the local fault to the phase it actually happened in, never
// call it "did not start", and still block overall success — except for
// interruption, which excludes the factory-fault attachment entirely
// (AC-005's explicit carve-out) and is unaffected by it.
func TestHarvestStoppedFactoryFailureMatrix(t *testing.T) {
	genericCleanup := errors.New("session broker close failed")
	recoveryFault := &SubmissionRecoveryError{RunID: 99, Attempt: "attempt-x", ReceiptID: 7, Story: "US-9", Phase: "recover receipt", Err: errors.New("store: pending submissions must retire in sequence")}

	tests := []struct {
		name          string
		runResult     *agent.RunResult
		factoryErr    error
		wantSucceeded bool
		wantPhase     string // "" means result.Err must stay nil
	}{
		{
			name:          "success, generic cleanup failure",
			runResult:     runResultAt(agent.OutcomeSucceeded, "", 0),
			factoryErr:    genericCleanup,
			wantSucceeded: false,
			wantPhase:     "local cleanup/finalization",
		},
		{
			name:          "success, scoped submission recovery failure",
			runResult:     runResultAt(agent.OutcomeSucceeded, "", 0),
			factoryErr:    recoveryFault,
			wantSucceeded: false,
			wantPhase:     "local submission recovery",
		},
		{
			name:          "execution failed, cleanup also failed",
			runResult:     runResultAt(agent.OutcomeFailed, "claude exited with status 1", 1),
			factoryErr:    genericCleanup,
			wantSucceeded: false,
			wantPhase:     "local cleanup/finalization",
		},
		{
			name:          "interruption excludes the factory fault",
			runResult:     runResultAt(agent.OutcomeInterrupted, "session limit reached", harness.SignalExitCode),
			factoryErr:    genericCleanup,
			wantSucceeded: false, // interrupted is never a success regardless of Err
			wantPhase:     "",
		},
		{
			name:          "baseline success, no factory fault",
			runResult:     runResultAt(agent.OutcomeSucceeded, "", 0),
			factoryErr:    nil,
			wantSucceeded: true,
			wantPhase:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)
			prep, run := newHarvestStoppedPrep(t, f)
			opts := f.opts

			result := Result{SessionID: f.session.ID, TriggerCommentID: 1}
			meta := &harness.Meta{}
			got, err := harvestStopped(t.Context(), opts, prep, result, tc.runResult, tc.factoryErr, meta, discardLogger())
			if err != nil {
				t.Fatalf("harvestStopped returned an unexpected error: %v", err)
			}

			// The process's own truthful outcome is always preserved,
			// independent of the factory fault's presence.
			if got.Outcome != tc.runResult.Outcome {
				t.Fatalf("Outcome = %v, want the real process outcome %v", got.Outcome, tc.runResult.Outcome)
			}
			if got.Harness == nil || got.Harness.ExitCode != tc.runResult.ExitCode {
				t.Fatalf("Harness exit code lost: %+v", got.Harness)
			}
			if got.Harness.Reason != tc.runResult.Reason {
				t.Fatalf("Harness.Reason = %q, want the real process reason %q", got.Harness.Reason, tc.runResult.Reason)
			}

			if tc.wantPhase == "" {
				if got.Err != nil {
					t.Fatalf("result.Err = %v, want nil", got.Err)
				}
			} else {
				if got.Err == nil {
					t.Fatal("result.Err is nil, want the attributed factory failure")
				}
				if !strings.Contains(got.Err.Error(), tc.wantPhase) {
					t.Fatalf("result.Err = %q, want it to name phase %q", got.Err.Error(), tc.wantPhase)
				}
				if !strings.Contains(got.Err.Error(), tc.factoryErr.Error()) {
					t.Fatalf("result.Err = %q, lost the original fault text %q", got.Err.Error(), tc.factoryErr.Error())
				}
				if strings.Contains(got.Err.Error(), "did not start") {
					t.Fatalf("a run that actually executed must never be reported as never having started: %q", got.Err.Error())
				}
				if !errors.Is(got.Err, tc.factoryErr) {
					t.Fatalf("result.Err does not wrap the original factory fault %v", tc.factoryErr)
				}
			}

			if got.Succeeded() != tc.wantSucceeded {
				t.Fatalf("Succeeded() = %v, want %v (result: %+v)", got.Succeeded(), tc.wantSucceeded, got)
			}

			// A real factory failure (any non-interruption case with a
			// factory fault) still blocks the run row and session from
			// reporting overall success, and reaches finish()'s save-only
			// gating -- exercised end-to-end here via the real DB write.
			storedRun, err := f.db.GetRun(t.Context(), run.ID)
			mustProgress(t, err)
			if tc.runResult.Outcome == agent.OutcomeSucceeded {
				if storedRun.Status != store.RunSucceeded {
					t.Fatalf("the process's own successful outcome was not recorded on the run row: %+v", storedRun)
				}
			}
			session, err := f.db.GetSession(t.Context(), f.session.ID)
			mustProgress(t, err)
			wantState := store.SessionFailed
			if tc.wantSucceeded {
				wantState = store.SessionIdle
			}
			if session.State != wantState {
				t.Fatalf("session state = %q, want %q", session.State, wantState)
			}
		})
	}
}

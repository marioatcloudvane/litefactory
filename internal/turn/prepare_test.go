package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// seedFailedRunWithReason writes the rows a previous, terminal run of this
// task would have left, carrying an arbitrary error reason rather than
// [store.ReasonAbandoned] — which is all [fixture.seedRun] can produce for a
// failed row. reported additionally moves the row on to [store.RunReported],
// the way [store.DB.CompleteRunReport] would once its comment had posted,
// while keeping the same non-empty error: a reported row that started life as
// a failure.
func seedFailedRunWithReason(
	t *testing.T, f *fixture, segmentOrdinal int, claudeSessionID string, commentID int64, mode, reason string, reported bool,
) store.Run {
	t.Helper()

	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, segmentOrdinal, claudeSessionID, store.AgentClaude)
	if err != nil {
		t.Fatalf("seed segment %d: %v", segmentOrdinal, err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{
		SessionID:        f.session.ID,
		SegmentID:        segment.ID,
		Mode:             mode,
		Agent:            store.AgentClaude,
		TriggerCommentID: commentID,
		LogPath:          func(int) string { return "/tmp/seeded.jsonl" },
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	exitCode := int64(2)
	run, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{
		Status: store.RunFailed, ExitCode: &exitCode, Error: reason,
	})
	if err != nil {
		t.Fatalf("seed run outcome: %v", err)
	}
	if reported {
		if run, err = f.db.CompleteRunReport(t.Context(), run.ID, commentID+1); err != nil {
			t.Fatalf("seed run report: %v", err)
		}
	}
	return run
}

// TestPrepareSetsPreviousFailedFields is US-4 (T-4.6): prepare populates
// [harness.PromptInput.PreviousFailed] and [PreviousReason] from the previous
// run's own stored [store.Run.Succeeded], and only on the resume path — a
// cold run has no predecessor to correct.
//
// The fields are observed indirectly, through the prompt that actually
// reached the recording runner and through the persisted metadata document
// (T-5.7's other half): [harness.PromptInput] itself never leaves this
// package, but [previousRunFailedNote] and the reason text are the only two
// things its two new fields can cause to appear.
func TestPrepareSetsPreviousFailedFields(t *testing.T) {
	t.Parallel()

	const previousFailedNoteFragment = "did not complete successfully"

	tests := []struct {
		name           string
		seed           func(t *testing.T, f *fixture)
		wantNote       bool
		wantReasonText string
	}{
		{
			name:     "no predecessor at all (cold run)",
			seed:     func(*testing.T, *fixture) {},
			wantNote: false,
		},
		{
			name: "predecessor succeeded",
			seed: func(t *testing.T, f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, "implement", store.RunSucceeded)
			},
			wantNote: false,
		},
		{
			name: "predecessor succeeded and was reported",
			seed: func(t *testing.T, f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, "implement", store.RunReported)
			},
			wantNote: false,
		},
		{
			name: "predecessor failed with a plain exit",
			seed: func(t *testing.T, f *fixture) {
				seedFailedRunWithReason(t, f, store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, "implement", "claude exited with status 2", false)
			},
			wantNote:       true,
			wantReasonText: "claude exited with status 2",
		},
		{
			name: "predecessor timed out",
			seed: func(t *testing.T, f *fixture) {
				seedFailedRunWithReason(t, f, store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, "implement", "timed out after 30m0s; the process subtree was killed", false)
			},
			wantNote:       true,
			wantReasonText: "timed out after 30m0s",
		},
		{
			name: "predecessor failed and was reported (error survives the move to reported)",
			seed: func(t *testing.T, f *fixture) {
				seedFailedRunWithReason(t, f, store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, "implement", "claude exited with status 1", true)
			},
			wantNote:       true,
			wantReasonText: "claude exited with status 1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *fixture
			runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			tc.seed(t, f)

			result := f.execute(f.request())
			if !result.Succeeded() {
				t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
			}
			if len(runner.requests) != 1 {
				t.Fatalf("claude ran %d times, want 1", len(runner.requests))
			}
			prompt := runner.requests[0].Prompt

			if got := strings.Contains(prompt, previousFailedNoteFragment); got != tc.wantNote {
				t.Errorf("previous-failed note present = %t, want %t:\n%s", got, tc.wantNote, prompt)
			}
			if tc.wantReasonText != "" && !strings.Contains(prompt, tc.wantReasonText) {
				t.Errorf("the prompt does not carry the predecessor's reason %q:\n%s", tc.wantReasonText, prompt)
			}

			// T-5.7's other half: what reached the subprocess is exactly what
			// the metadata document persisted.
			meta := f.readMeta(runner.requests[0].RunOrdinal)
			if meta.Prompt != prompt {
				t.Errorf("the metadata document does not record the prompt that was sent")
			}
			if got := strings.Contains(meta.Prompt, previousFailedNoteFragment); got != tc.wantNote {
				t.Errorf("previous-failed note in the persisted prompt = %t, want %t", got, tc.wantNote)
			}
		})
	}
}

// TestResumeDecisionTrueForAFailedClaudePredecessor is AC-4.2, named directly:
// resume stays enabled after a failed run on the same segment. It is a
// dedicated pin of the one case US-4's cross-story constraint calls out by
// name, alongside the fuller sweep in TestResumeDecisionComesFromTheRunsTable
// (resume_test.go).
func TestResumeDecisionTrueForAFailedClaudePredecessor(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, "implement", store.RunFailed)

	result := f.execute(f.request())

	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if len(runner.requests) != 1 || runner.requests[0].Path != harness.PathResume {
		t.Fatalf("run path = %v, want exactly one request on %s", runner.requests, harness.PathResume)
	}
}

// TestPrepareDoesNotWrapSessionRunningInWithoutCancel is US-4 (T-4.7)'s
// testable half: prepare's own doc comment is explicit that "last store
// write" is an invariant about statement order that "is not something a test
// can enforce from outside" — but whether prepare calls
// context.WithoutCancel anywhere is a property of its syntax tree, in the
// same style [TestTurnImposesNoAmbientDeadline] already checks for
// WithTimeout/WithDeadline. SetSessionState(ctx, ..., SessionRunning) taking
// the ordinary, cancellable ctx — rather than a bookkeeping context laundered
// through WithoutCancel, the way runAndHarvest's own writes are — is what
// keeps a daemon shutdown mid-prepare from forcing a write the caller no
// longer wants.
func TestPrepareDoesNotWrapSessionRunningInWithoutCancel(t *testing.T) {
	t.Parallel()

	funcs := packageFuncs(t)
	fn, ok := funcs["prepare"]
	if !ok {
		t.Fatal("prepare is not declared in package turn")
	}
	if fn.file != "prepare.go" {
		t.Errorf("prepare is declared in %s, want prepare.go", fn.file)
	}
	if got := countCalls(fn.decl, "WithoutCancel"); got != 0 {
		t.Errorf("prepare calls context.WithoutCancel %d times, want 0: its last store write "+
			"must observe the caller's own cancellation, unlike runAndHarvest's bookkeeping writes", got)
	}
}

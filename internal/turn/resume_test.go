package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// The comment ids the seeded runs were triggered by. They are not the id the
// turn under test arrives with: a comment that has already produced a run is a
// replay and never produces a second one (AC-3.4).
const (
	seededComment      = int64(8001)
	otherSeededComment = int64(8002)
)

// recordingRunner is a claude that says what it was asked to do and answers the
// way the test needs. Every resume assertion in this file is made against the
// requests it recorded, because the request is where the decision lands.
func recordingRunner(
	fn func(req harness.Request) (*harness.RunResult, error),
) *stubRunner {
	return newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return fn(req)
	})
}

// TestResumeDecisionComesFromTheRunsTable is AC-SR-2.3.
//
// The question the decision answers is "has claude already been invoked with
// this session id?", and the runs table is the only place that is written down:
// a row on this segment that has stopped means a subprocess was started with
// it, whatever the run then did. No column was added for this and none is
// needed.
func TestResumeDecisionComesFromTheRunsTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// seed runs the rows a previous turn would have left, if any.
		seed func(f *fixture)
		want harness.RunPath
	}{
		{
			name: "no prior runs at all",
			seed: func(*fixture) {},
			want: harness.PathCold,
		},
		{
			name: "one prior run that succeeded",
			seed: func(f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, store.ModeSpike, store.RunSucceeded)
			},
			want: harness.PathResume,
		},
		{
			name: "one prior run that failed",
			seed: func(f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, store.ModeSpike, store.RunFailed)
			},
			want: harness.PathResume,
		},
		{
			name: "one prior run that has been reported",
			seed: func(f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, store.ModeSpike, store.RunReported)
			},
			want: harness.PathResume,
		},
		{
			// Either a run this daemon is conducting right now, which cannot be
			// its own predecessor, or one a killed daemon left behind — and
			// that one becomes failed the moment reconciliation sees it.
			name: "one prior run still marked running",
			seed: func(f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, store.ModeSpike, store.RunRunning)
			},
			want: harness.PathCold,
		},
		{
			name: "several prior runs, the newest still running",
			seed: func(f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, store.ModeSpike, store.RunReported)
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					otherSeededComment, store.ModeSpike, store.RunRunning)
			},
			want: harness.PathResume,
		},
		{
			// A different conversation is not evidence about this one. This
			// is the state a rotation leaves behind (US-253): the run lives
			// on the segment it actually belongs to, and the session has
			// since moved on to a later segment with no run of its own yet.
			name: "a prior run on a different segment",
			seed: func(f *fixture) {
				f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
					seededComment, store.ModeSpike, store.RunSucceeded)
				nextRef, err := naming.SessionRef(f.ref(), store.FirstSegmentOrdinal+1)
				if err != nil {
					f.t.Fatalf("derive next segment ref: %v", err)
				}
				if _, err := f.db.GetOrCreateSegment(f.t.Context(), f.session.ID, store.FirstSegmentOrdinal+1, nextRef, store.AgentClaude); err != nil {
					f.t.Fatalf("seed segment %d: %v", store.FirstSegmentOrdinal+1, err)
				}
			},
			want: harness.PathCold,
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
			tc.seed(f)

			if result := f.execute(f.request()); !result.Succeeded() {
				t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
			}
			if runner.callCount() != 1 {
				t.Fatalf("claude ran %d times, want 1", runner.callCount())
			}
			if got := runner.requests[0].Path; got != tc.want {
				t.Errorf("run path = %s, want %s", got, tc.want)
			}
			if got := f.readMeta(runner.requests[0].RunOrdinal).Resumed; got != (tc.want == harness.PathResume) {
				t.Errorf("meta resumed = %t on the %s path", got, tc.want)
			}
		})
	}
}

// A resumed run sends the delta and nothing else (AC-SR-2.4). The prompt is
// asserted where it is actually delivered, so this covers prepare's wiring of
// the flag as well as the assembly itself.
func TestResumedRunSendsTheDeltaPrompt(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, store.ModeSpike, store.RunReported)

	f.execute(f.request())

	prompt := runner.requests[0].Prompt
	if !strings.Contains(prompt, "# litefactory run 0002 of ") {
		t.Errorf("the delta prompt does not name the run:\n%s", prompt)
	}
	if !strings.Contains(prompt, "- Feature slug: "+testSlug) {
		t.Errorf("the delta prompt dropped the feature slug, which is the run's insurance (D-3):\n%s", prompt)
	}
	for _, unwanted := range []string{testIssueTitle, "- Branch:", "- Working directory:", "feature name or ticket id"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("the delta prompt repeats %q, which the conversation has already seen:\n%s", unwanted, prompt)
		}
	}
	// AC-3.11 still holds on this path: the document records what was sent.
	if meta := f.readMeta(2); meta.Prompt != prompt {
		t.Errorf("the metadata document does not record the prompt that was sent:\n%s", meta.Prompt)
	}
}

// AC-SR-2.5, through the turn: the previous run's mode comes off the row the
// resume decision already read, so a conversation that began as a /spike and is
// now being asked to infer is told what inferring means.
func TestResumedRunReEmitsGuidanceWhenTheModeChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		previousMode string
		wantGuidance bool
	}{
		{name: "spike then infer", previousMode: store.ModeSpike, wantGuidance: true},
		{name: "infer then infer", previousMode: harness.InferMode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *fixture
			runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
				seededComment, tc.previousMode, store.RunReported)

			// A comment that names no command: the mode is inferred.
			req := f.request()
			req.Trigger.Body = "have another look at the health endpoint please"
			req.Command = ParseCommand(req.Trigger.Body)
			req.Mode = ""
			f.execute(req)

			if got := runner.requests[0].Mode; got != harness.InferMode {
				t.Fatalf("mode = %q, want %q", got, harness.InferMode)
			}
			got := strings.Contains(runner.requests[0].Prompt, "named no command")
			if got != tc.wantGuidance {
				t.Errorf("mode guidance present = %t, want %t\n%s",
					got, tc.wantGuidance, runner.requests[0].Prompt)
			}
		})
	}
}

// TestResumeRefusalRerunsOnceCold is AC-SR-2.8, and it is the reason the store
// being the *intent* signal is survivable.
//
// The rows say a conversation exists; the transcript lives on whichever machine
// wrote it. When the two disagree, claude refuses — and without this rerun the
// task is wedged forever, because every later run asks for the same missing
// conversation and no comment on the issue can change that.
func TestResumeRefusalRerunsOnceCold(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		if req.Path == harness.PathResume {
			return refused(req, f.paths.Home())
		}
		writeInWorktree(t, req, "health.go", "package api // written by the rerun\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, store.ModeSpike, store.RunReported)

	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("the cold rerun did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if runner.callCount() != 2 {
		t.Fatalf("claude ran %d times, want exactly 2: the resume and one cold rerun", runner.callCount())
	}

	first, second := runner.requests[0], runner.requests[1]
	if first.Path != harness.PathResume || second.Path != harness.PathColdAfterResumeRefused {
		t.Errorf("paths = %s then %s, want resume then cold_after_resume_refused", first.Path, second.Path)
	}
	// The rerun is this run, not a new one: same row, same ordinal, same log.
	if second.RunOrdinal != first.RunOrdinal {
		t.Errorf("the rerun took ordinal %d, want the same %d", second.RunOrdinal, first.RunOrdinal)
	}
	if runs := f.runs(); len(runs) != 2 {
		t.Errorf("the task has %d run rows, want the seeded one plus this run", len(runs))
	}
	if result.Run.Ordinal != first.RunOrdinal {
		t.Errorf("the finished row is ordinal %d, want %d", result.Run.Ordinal, first.RunOrdinal)
	}

	// The second prompt is the cold one, and it is what the document records:
	// the abandoned delta was never sent to a conversation that existed.
	if !strings.Contains(second.Prompt, testIssueTitle) {
		t.Errorf("the rerun's prompt is not the cold one:\n%s", second.Prompt)
	}
	if strings.Contains(first.Prompt, testIssueTitle) {
		t.Errorf("the first attempt sent the cold prompt, not the delta:\n%s", first.Prompt)
	}
	meta := f.readMeta(second.RunOrdinal)
	if meta.Prompt != second.Prompt {
		t.Errorf("the document records the abandoned prompt rather than what was sent:\n%s", meta.Prompt)
	}
	if meta.Resumed {
		t.Error("the document says the run resumed; it ran cold after being refused")
	}
	if !strings.Contains(f.logs.String(), "rerunning this run cold") {
		t.Errorf("the fallback is not in the log, so it cannot be audited:\n%s", f.logs)
	}
}

// The rerun is for one condition and one only. Every other way a run can fail
// is reported, not retried: rerunning a run that got as far as an init event
// would throw away the conversation it was having, and rerunning a timeout
// would double an already long turn.
func TestResumeRefusalDoesNotFireOnOtherFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		respond func(req harness.Request, home string) (*harness.RunResult, error)
	}{
		{
			name: "a resumed run that failed after its init event",
			respond: func(req harness.Request, home string) (*harness.RunResult, error) {
				result := succeeded(req, home)
				result.Outcome = harness.OutcomeFailed
				result.Reason = "claude exited with status 2"
				result.ExitCode = 2
				return result, nil
			},
		},
		{
			name: "a resumed run that timed out",
			respond: func(req harness.Request, home string) (*harness.RunResult, error) {
				result := succeeded(req, home)
				result.Outcome = harness.OutcomeTimedOut
				result.Reason = "timed out after 30s; the process subtree was killed"
				result.ExitCode = harness.SignalExitCode
				result.Model = ""
				result.HasResult = false
				return result, nil
			},
		},
		{
			name: "a resumed run abandoned by a shutting-down daemon",
			respond: func(req harness.Request, home string) (*harness.RunResult, error) {
				result := succeeded(req, home)
				result.Outcome = harness.OutcomeAbandoned
				result.Reason = "abandoned: the caller's run context ended before claude finished"
				result.ExitCode = harness.SignalExitCode
				result.Model = ""
				result.HasResult = false
				return result, nil
			},
		},
		{
			name: "a resumed run that never started",
			respond: func(harness.Request, string) (*harness.RunResult, error) {
				return nil, fmt.Errorf("%w: /usr/local/bin/claude: permission denied", harness.ErrStartFailed)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *fixture
			runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
				return tc.respond(req, f.paths.Home())
			})
			f = newFixture(t, runner)
			f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
				seededComment, store.ModeSpike, store.RunReported)

			result := f.execute(f.request())

			if runner.callCount() != 1 {
				t.Fatalf("claude ran %d times, want 1: only a refused resume is rerun", runner.callCount())
			}
			if result.Succeeded() {
				t.Errorf("the failure was reported as a success: %+v", result)
			}
			if got := f.readMeta(runner.requests[0].RunOrdinal).Resumed; !got {
				t.Error("meta resumed = false, but the run was on the resume path and stayed there")
			}
		})
	}
}

// One rerun is the whole budget. A cold run is never classified as a refusal,
// so a second one cannot happen against a real claude — but "cannot happen" is
// how an infinite loop gets written, and this is the assertion that there is no
// third attempt if it does.
func TestResumeRefusalDoesNotLoop(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return refused(req, f.paths.Home())
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, store.ModeSpike, store.RunReported)

	result := f.execute(f.request())

	if runner.callCount() != 2 {
		t.Fatalf("claude ran %d times, want exactly 2", runner.callCount())
	}
	if result.Succeeded() {
		t.Error("a run that was refused twice was reported as a success")
	}
	if !result.Reportable() {
		t.Error("the failure owes the issue a comment (AC-4.1) and is not reportable")
	}
	runs := f.runs()
	if len(runs) != 2 || runs[1].Status != store.RunFailed {
		t.Errorf("runs = %+v, want the seeded row plus one failed run", runs)
	}
}

// The sentinel is matched with errors.Is, never by string. Wrapping it is how
// the harness explains which session id was refused, and a match that broke on
// the wrapping would silently stop the fallback from ever firing.
func TestResumeRefusalIsMatchedThroughWrapping(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		if req.Path == harness.PathResume {
			result, err := refused(req, f.paths.Home())
			return result, fmt.Errorf("run 2 of task github:acme/acme-api#123: %w", err)
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, store.ModeSpike, store.RunReported)

	if result := f.execute(f.request()); !result.Succeeded() {
		t.Fatalf("the wrapped sentinel was not recognised: %+v\nlogs:\n%s", result, f.logs)
	}
	if runner.callCount() != 2 {
		t.Errorf("claude ran %d times, want 2", runner.callCount())
	}
	if _, err := refused(harness.Request{}, ""); !errors.Is(err, harness.ErrSessionNotResumable) {
		t.Error("the fixture's refusal does not carry the sentinel, so this test asserts nothing")
	}
}

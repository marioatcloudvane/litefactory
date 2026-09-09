package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Protocol fixture provenance: synthetic JSONL based on Codex rust-v0.104.0:
// https://raw.githubusercontent.com/openai/codex/rust-v0.104.0/codex-rs/exec/src/event_processor_with_jsonl_output.rs
// StreamError emits error; terminal critical-error state chooses turn.failed
// versus turn.completed. Retry wording originates in run_sampling_request and
// notify_stream_error in the same tag's codex-rs/core/src/codex.rs.
// When upgrading supported Codex, recheck those mappings at the supported tag
// and update this matrix if the contract changes. This is not an assertion about
// the unknown incident binary version; tests use no installed/live provider.
const (
	reconnectNotice = `{"type":"error","message":"Reconnecting... 2/5 (stream disconnected before completion: invalid peer certificate: UnknownIssuer)"}`
	otherNotice     = `{"type":"error","message":"a diagnostic with unrelated wording"}`
	completedEvent  = `{"type":"turn.completed"}`
)

func TestRunnerRecoverableDiagnostics(t *testing.T) {
	t.Parallel()
	const final = "exact final report\nwith 'quotes' and trailing space "
	for _, tc := range []struct {
		name          string
		before, after []string
	}{
		{"one reconnect before", []string{reconnectNotice}, nil},
		{"unrelated before", []string{otherNotice}, nil},
		{"reconnect after", nil, []string{reconnectNotice}},
		{"unrelated after", nil, []string{otherNotice}},
		{"multiple around final", []string{reconnectNotice, otherNotice}, []string{otherNotice, reconnectNotice}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lines := append([]string{}, tc.before...)
			lines = append(lines, agentMessage(t, final))
			lines = append(lines, tc.after...)
			lines = append(lines, completedEvent, otherNotice)
			runner, _ := fakeRunner(t, eventScript(lines, 0))

			result, err := runner.Run(t.Context(), request(t, agent.PathCold))

			if err != nil {
				t.Fatal(err)
			}
			if !result.Succeeded() || !result.HasResult || result.ResultText != final {
				t.Fatalf("result = %+v, want successful exact final %q", result, final)
			}
			raw := string(mustRead(t, result.LogPath))
			if raw != strings.Join(lines, "\n")+"\n" {
				t.Fatalf("raw log = %q, want all original events", raw)
			}
		})
	}
}

func TestRunnerTerminalEvidence(t *testing.T) {
	t.Parallel()
	const final = "the agent response"
	details := []struct {
		name, event, reason string
	}{
		{"nested", `{"type":"turn.failed","error":{"message":"provider failed"}}`, "provider failed"},
		{"top level", `{"type":"turn.failed","message":"legacy detail"}`, "legacy detail"},
		{"missing", `{"type":"turn.failed"}`, "turn.failed"},
		{"malformed nested", `{"type":"turn.failed","error":42}`, "turn.failed"},
		{"malformed nested message", `{"type":"turn.failed","error":{"message":[]}}`, "turn.failed"},
		{"malformed top level", `{"type":"turn.failed","message":{}}`, "turn.failed"},
		{"empty", `{"type":"turn.failed","error":{"message":""}}`, "turn.failed"},
		{"null", `{"type":"turn.failed","error":null,"message":null}`, "turn.failed"},
		{"fallback top level", `{"type":"turn.failed","error":false,"message":"fallback detail"}`, "fallback detail"},
	}
	for _, detail := range details {
		for _, order := range []string{"no completion", "completion before", "completion after"} {
			for _, exit := range []int{0, 7} {
				t.Run(fmt.Sprintf("%s/%s/exit%d", detail.name, order, exit), func(t *testing.T) {
					t.Parallel()
					lines := []string{agentMessage(t, final), reconnectNotice}
					if order == "completion before" {
						lines = append(lines, completedEvent)
					}
					lines = append(lines, detail.event, otherNotice)
					if order == "completion after" {
						lines = append(lines, completedEvent)
					}
					runner, _ := fakeRunner(t, eventScript(lines, exit))

					result, err := runner.Run(t.Context(), request(t, agent.PathCold))

					if err != nil {
						t.Fatal(err)
					}
					if result.Outcome != agent.OutcomeFailed || result.ExitCode != exit || result.ResultText != final {
						t.Fatalf("result = %+v", result)
					}
					if result.Reason == "" || strings.Contains(result.Reason, "exited with status 0") {
						t.Fatalf("untruthful reason %q", result.Reason)
					}
					if exit == 0 && !strings.Contains(result.Reason, detail.reason) {
						t.Fatalf("reason %q missing %q", result.Reason, detail.reason)
					}
				})
			}
		}
	}
	for _, tc := range []struct {
		name     string
		complete bool
		exit     int
		want     agent.Outcome
		reason   string
	}{
		{"missing completion", false, 0, agent.OutcomeNoResult, "without a turn.completed"},
		{"nonzero completed", true, 7, agent.OutcomeFailed, "status 7"},
		{"nonzero incomplete", false, 7, agent.OutcomeFailed, "status 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lines := []string{reconnectNotice, agentMessage(t, final), otherNotice}
			if tc.complete {
				lines = append(lines, completedEvent)
			}
			runner, _ := fakeRunner(t, eventScript(lines, tc.exit))
			result, err := runner.Run(t.Context(), request(t, agent.PathCold))
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != tc.want || result.ExitCode != tc.exit || !strings.Contains(result.Reason, tc.reason) {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestClassifyOutcomeContextPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		exit              int
		completed, failed bool
		caller, run       error
		want              agent.Outcome
		wantExit          int
	}{
		{"success", 0, true, false, nil, nil, agent.OutcomeSucceeded, 0},
		{"completed cancellation race", 0, true, false, context.Canceled, context.Canceled, agent.OutcomeSucceeded, 0},
		{"completed timeout race", 0, true, false, nil, context.DeadlineExceeded, agent.OutcomeSucceeded, 0},
		{"caller cancellation", 7, false, false, context.Canceled, context.Canceled, agent.OutcomeAbandoned, agent.SignalExitCode},
		{"caller deadline", 7, false, false, context.DeadlineExceeded, context.DeadlineExceeded, agent.OutcomeAbandoned, agent.SignalExitCode},
		{"run timeout", 7, false, false, nil, context.DeadlineExceeded, agent.OutcomeTimedOut, agent.SignalExitCode},
		{"caller takes precedence", 7, false, false, context.Canceled, context.DeadlineExceeded, agent.OutcomeAbandoned, agent.SignalExitCode},
		{"failed completion cancelled", 0, true, true, context.Canceled, context.Canceled, agent.OutcomeAbandoned, agent.SignalExitCode},
		{"failed completion timeout", 0, true, true, nil, context.DeadlineExceeded, agent.OutcomeTimedOut, agent.SignalExitCode},
		{"nonzero completed cancelled", 7, true, false, context.Canceled, context.Canceled, agent.OutcomeAbandoned, agent.SignalExitCode},
		{"nonzero completed timeout", 7, true, false, nil, context.DeadlineExceeded, agent.OutcomeTimedOut, agent.SignalExitCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := agent.RunResult{ExitCode: tc.exit}
			stream := streamState{completed: tc.completed, failed: tc.failed}
			classifyOutcome(&result, stream, tc.caller, tc.run, 30*time.Second)
			if result.Outcome != tc.want || result.ExitCode != tc.wantExit {
				t.Fatalf("result = %+v, want %v exit %d", result, tc.want, tc.wantExit)
			}
			if tc.want != agent.OutcomeSucceeded && result.Reason == "" {
				t.Fatal("missing abnormal-outcome reason")
			}
		})
	}
}

func TestStructuredConsumersRecoverableDiagnostics(t *testing.T) {
	t.Parallel()
	for _, consumer := range []struct {
		name, payload string
		invoke        func(*testing.T, *Runner) error
	}{
		{"Classify", "implement", func(t *testing.T, r *Runner) error {
			got, err := r.Classify(t.Context(), agent.Classification{WorkingDirectory: t.TempDir(), Prompt: "implement"})
			if err == nil && got != agent.ModeImplement {
				t.Fatalf("mode = %q", got)
			}
			return err
		}},
		{"ProposeSchedule", `{"items":[{"payload":"/implement T-1","due_at":"2026-09-05T01:00:00Z"}]}`, func(t *testing.T, r *Runner) error {
			got, err := r.ProposeSchedule(t.Context(), agent.ScheduleProposalRequest{Description: "schedule", ReceivedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)})
			if err == nil && (len(got.Items) != 1 || got.Items[0].Payload != "/implement T-1" || !got.Items[0].DueAt.Equal(time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC))) {
				t.Fatalf("schedule = %+v", got)
			}
			return err
		}},
		{"ProposeConflictResolution", `{"version":1,"files":[{"path":"a.go","regions":[{"id":"a-1","resolution":"package a\n"}]}]}`, func(t *testing.T, r *Runner) error {
			got, err := r.ProposeConflictResolution(t.Context(), agent.ConflictResolutionRequest{Branch: "feature", Base: "main", Files: []agent.ConflictFileContent{{Path: "a.go", Content: "conflicting content", Regions: []agent.ConflictRegionContent{{ID: "a-1", Content: "conflicting content"}}}}})
			if err == nil {
				if len(got.Files) != 1 || got.Files[0].Path != "a.go" || len(got.Files[0].Regions) != 1 {
					t.Fatalf("resolution = %+v", got)
				}
				if r := got.Files[0].Regions[0].Resolution; r == nil || *r != "package a\n" {
					t.Fatalf("region resolution = %v", r)
				}
			}
			return err
		}},
	} {
		t.Run(consumer.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name          string
				before, after []string
				exit          int
				wantErr       bool
			}{
				{"recovered before", []string{reconnectNotice, otherNotice}, []string{completedEvent}, 0, false},
				{"recovered after payload", nil, []string{otherNotice, reconnectNotice, completedEvent}, 0, false},
				{"recovered after completion", []string{otherNotice}, []string{completedEvent, reconnectNotice}, 0, false},
				{"failure after completion", nil, []string{completedEvent, `{"type":"turn.failed","error":{"message":"failed"}}`}, 0, true},
				{"failure before completion", []string{`{"type":"turn.failed"}`}, []string{completedEvent}, 0, true},
				{"malformed failure detail", nil, []string{`{"type":"turn.failed","error":[]}`, completedEvent}, 0, true},
				{"absent completion", []string{reconnectNotice}, []string{otherNotice}, 0, true},
				{"nonzero exit", []string{reconnectNotice}, []string{completedEvent}, 7, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					lines := append([]string{}, tc.before...)
					lines = append(lines, agentMessage(t, consumer.payload))
					lines = append(lines, tc.after...)
					runner, _ := fakeRunner(t, eventScript(lines, tc.exit))
					err := consumer.invoke(t, runner)
					if (err != nil) != tc.wantErr {
						t.Fatalf("err = %v, wantErr %t", err, tc.wantErr)
					}
				})
			}
		})
	}
}

func agentMessage(t *testing.T, text string) string {
	t.Helper()
	encoded, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	return `{"type":"item.completed","item":{"type":"agent_message","text":` + string(encoded) + "}}"
}

func eventScript(lines []string, exit int) string {
	var script strings.Builder
	for _, line := range lines {
		script.WriteString("printf '%s\\n' '")
		script.WriteString(strings.ReplaceAll(line, "'", "'\"'\"'"))
		script.WriteString("'\n")
	}
	fmt.Fprintf(&script, "exit %d\n", exit)
	return script.String()
}

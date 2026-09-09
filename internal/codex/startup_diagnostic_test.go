package codex

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Synthetic stderr and events exercise classification, not live capability.
const startupPermissionSignature = "Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)"
const startupPathWarning = "WARNING: proceeding, even though we could not create PATH aliases: Operation not permitted (os error 1)"

func TestRunnerStartupPermissionDiagnostic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, stderr string
		events       []string
		exit         int
		outcome      agent.Outcome
		hint         bool
	}{
		{"signature/failed hint", startupPermissionSignature, nil, 7, agent.OutcomeFailed, true},
		{"CRLF signature/failed hint", startupPermissionSignature + "\r", nil, 7, agent.OutcomeFailed, true},
		{"zero exit signature/no result", startupPermissionSignature, nil, 0, agent.OutcomeNoResult, false},
		{"warning and signature/failed hint", startupPathWarning + "\n" + startupPermissionSignature, nil, 7, agent.OutcomeFailed, true},
		{"success/no hint", "", []string{completedEvent}, 0, agent.OutcomeSucceeded, false},
		{"warning success/no hint", startupPathWarning, []string{completedEvent}, 0, agent.OutcomeSucceeded, false},
		{"signature success/no hint", startupPermissionSignature, []string{completedEvent}, 0, agent.OutcomeSucceeded, false},
		{"warning failure/no hint", startupPathWarning, nil, 7, agent.OutcomeFailed, false},
		{"arbitrary permission/no hint", "Error: opening cache: Operation not permitted (os error 1)", nil, 7, agent.OutcomeFailed, false},
		{"unrelated initialization/no hint", "Error: failed to initialize in-process app-server client: connection refused", nil, 7, agent.OutcomeFailed, false},
		{"separate cause/no hint", "Error: failed to initialize in-process app-server client: connection refused\nOperation not permitted (os error 1)", nil, 7, agent.OutcomeFailed, false},
		{"quoted stderr/no hint", "example: " + startupPermissionSignature, nil, 7, agent.OutcomeFailed, false},
		{"generated quotation/no hint", "", []string{agentMessage(t, startupPermissionSignature)}, 7, agent.OutcomeFailed, false},
		{"authentication/no hint", "Error: authentication failed: please login", nil, 7, agent.OutcomeFailed, false},
		{"started thread/no hint", startupPermissionSignature, []string{`{"type":"thread.started","thread_id":"synthetic-thread"}`}, 7, agent.OutcomeFailed, false},
		{"terminal failure/precedence", startupPermissionSignature, []string{`{"type":"turn.failed","error":{"message":"provider failed"}}`}, 0, agent.OutcomeFailed, false},
		{"terminal nonzero/precedence", startupPermissionSignature, []string{`{"type":"turn.failed","error":{"message":"provider failed"}}`}, 7, agent.OutcomeFailed, false},
		{"quota/precedence", startupPermissionSignature, []string{`{"type":"turn.failed","error":{"message":"You've hit your usage limit. try again at 10:24 PM."}}`}, 7, agent.OutcomeInterrupted, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			script := "cat <<'STDERR' >&2\n" + tc.stderr + "\nSTDERR\n" + eventScript(tc.events, tc.exit)
			runner, _ := fakeRunner(t, script)
			got, err := runner.Run(t.Context(), request(t, agent.PathCold))
			if err != nil {
				t.Fatal(err)
			}
			if got.Outcome != tc.outcome || got.ExitCode != tc.exit {
				t.Fatalf("outcome=%s exit=%d, want %s exit=%d", got.Outcome, got.ExitCode, tc.outcome, tc.exit)
			}
			if (got.Diagnostic != "") != tc.hint {
				t.Fatalf("diagnostic=%q, want hint=%v", got.Diagnostic, tc.hint)
			}
			if tc.hint {
				for _, want := range []string{"sandbox", "nested", "fixture", "provenance"} {
					if !strings.Contains(strings.ToLower(got.Diagnostic), want) {
						t.Errorf("diagnostic %q missing %q", got.Diagnostic, want)
					}
				}
				if !strings.Contains(got.Reason, "7") || !strings.Contains(got.Stderr, tc.stderr) {
					t.Fatalf("lost exit reason or stderr: %+v", got)
				}
			}
		})
	}
}

func TestClassifyOutcomeStartupPermissionContextPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		caller, run error
		want        agent.Outcome
	}{
		{"cancellation", context.Canceled, context.Canceled, agent.OutcomeAbandoned},
		{"caller deadline", context.DeadlineExceeded, context.DeadlineExceeded, agent.OutcomeAbandoned},
		{"timeout", nil, context.DeadlineExceeded, agent.OutcomeTimedOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agent.RunResult{ExitCode: 7, Stderr: startupPermissionSignature}
			classifyOutcome(&got, streamState{}, tc.caller, tc.run, time.Second)
			if got.Outcome != tc.want || got.ExitCode != agent.SignalExitCode || got.Diagnostic != "" {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

package codex

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestClassifyOutcomeMCPStartup(t *testing.T) {
	const direct = "Error: required MCP servers failed to initialize: litefactory: Mcp error: -32602: Invalid tool parameters"
	const thread = "Error: thread/start: thread/start failed: error creating thread: Fatal error: Failed to initialize session: required MCP servers failed to initialize: litefactory: Mcp error: -32602: Invalid tool parameters"
	for _, tc := range []struct {
		name, stderr string
		stream       streamState
		exit         int
		caller, run  error
		want         agent.Outcome
		hint         bool
	}{
		{"direct", direct, streamState{}, 7, nil, nil, agent.OutcomeFailed, true},
		{"thread", thread, streamState{}, 7, nil, nil, agent.OutcomeFailed, true},
		{"CRLF", direct + "\r\n", streamState{}, 7, nil, nil, agent.OutcomeFailed, true},
		{"quotation", "example: " + direct, streamState{}, 7, nil, nil, agent.OutcomeFailed, false},
		{"no server cause", "Error: required MCP servers failed to initialize: ", streamState{}, 7, nil, nil, agent.OutcomeFailed, false},
		{"zero exit", direct, streamState{}, 0, nil, nil, agent.OutcomeNoResult, false},
		{"successful events", direct, streamState{completed: true, events: 1}, 0, nil, nil, agent.OutcomeSucceeded, false},
		{"started events", direct, streamState{events: 1}, 7, nil, nil, agent.OutcomeFailed, false},
		{"cancel", direct, streamState{}, 7, context.Canceled, context.Canceled, agent.OutcomeAbandoned, false},
		{"timeout", direct, streamState{}, 7, nil, context.DeadlineExceeded, agent.OutcomeTimedOut, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agent.RunResult{ExitCode: tc.exit, Stderr: tc.stderr}
			classifyOutcome(&got, tc.stream, tc.caller, tc.run, time.Second)
			if got.Outcome != tc.want || (got.Diagnostic != "") != tc.hint {
				t.Fatalf("result=%+v want outcome=%s hint=%v", got, tc.want, tc.hint)
			}
			if got.Stderr != tc.stderr {
				t.Fatal("original stderr changed")
			}
			if tc.hint && (!strings.Contains(got.Diagnostic, "required MCP") || !strings.Contains(got.Reason, "status 7") || got.ExitCode != 7) {
				t.Fatalf("lost startup diagnosis or process cause: %+v", got)
			}
		})
	}
}

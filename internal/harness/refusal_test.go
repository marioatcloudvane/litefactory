package harness

import (
	"encoding/json"
	"errors"
	"testing"
)

// Extracted facts from RUNTIME_EVIDENCE.json: claude_refusal, source #195 run
// 12 line 1. The original transcript is not embedded. This minimal envelope is
// reconstructed; the terminal counters and usage shape are historical facts.
// Refused-process version and actual model remain unknown (the fallback version
// does not establish either). All variations below are synthetic negatives.
const historicalRefusalFacts = `{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":0,"total_cost_usd":0,"usage":{"output_tokens_details":{"thinking_tokens":0},"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},"inference_geo":"","iterations":[],"speed":"standard"}}`

func TestProvenZeroCostRefusalEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		field  string
		value  any
		remove bool
		before string
		after  string
		mutate func(*RunResult)
		cold   bool
		want   bool
	}{
		{name: "historical affirmative terminal/classified with unknown model", want: true},
		{name: "missing cost/unknown", field: "total_cost_usd", remove: true},
		{name: "null cost/unknown", field: "total_cost_usd"},
		{name: "positive cost/not zero refusal", field: "total_cost_usd", value: 0.1},
		{name: "negative cost/invalid evidence", field: "total_cost_usd", value: -1},
		{name: "missing turns/unknown", field: "num_turns", remove: true},
		{name: "null turns/unknown", field: "num_turns"},
		{name: "positive turns/work occurred", field: "num_turns", value: 1},
		{name: "negative turns/invalid evidence", field: "num_turns", value: -1},
		{name: "success terminal/not refusal", field: "is_error", value: false},
		{name: "cold invocation/not resume refusal", cold: true},
		{name: "no terminal result/unknown", mutate: func(r *RunResult) { r.HasResult = false }},
		{name: "model resolved/not structural refusal", mutate: func(r *RunResult) { r.Model = "reported-model" }},
		{name: "successful exit/not refusal", mutate: func(r *RunResult) { r.ExitCode = 0 }},
		{name: "interrupted outcome/uncertain", mutate: func(r *RunResult) { r.Outcome = OutcomeAbandoned }},
		{name: "broken stream/uncertain", mutate: func(r *RunResult) { r.StreamError = errors.New("interrupted stdout") }},
		{name: "assistant message/contrary work", before: `{"type":"assistant","message":{"content":[{"type":"text","text":"worked"}]}}`},
		{name: "tool activity/contrary work", before: `{"type":"tool_use","name":"Read"}`},
		{name: "second terminal/ambiguous", after: historicalRefusalFacts},
		{name: "malformed trailing record/uncertain", after: `{"type":`},
		{name: "input usage/contrary work", field: "usage", value: map[string]any{"input_tokens": 1}},
		{name: "output usage/contrary work", field: "usage", value: map[string]any{"output_tokens": 1}},
		{name: "tool usage/contrary work", field: "usage", value: map[string]any{"server_tool_use": map[string]any{"web_search_requests": 1}}},
		{name: "model usage/contrary work", field: "modelUsage", value: map[string]any{"model": map[string]any{"inputTokens": 1}}},
		{name: "unsupported usage/uncertain", field: "usage", value: "unknown"},
		{name: "absent usage/terminal cost still affirmative", field: "usage", remove: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var fields map[string]any
			if err := json.Unmarshal([]byte(historicalRefusalFacts), &fields); err != nil {
				t.Fatal(err)
			}
			if tt.field != "" {
				if tt.remove {
					delete(fields, tt.field)
				} else {
					fields[tt.field] = tt.value
				}
			}
			line, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			stream, _ := consumeLines(t, tt.before, string(line), tt.after)
			if stream.result == nil {
				t.Fatal("terminal facts not parsed")
			}
			event := stream.result
			result := &RunResult{Outcome: OutcomeFailed, ExitCode: 1, HasResult: true, IsError: event.IsError, HasCost: event.HasCost, CostUSD: event.TotalCostUSD, HasNumTurns: event.HasNumTurns, NumTurns: event.NumTurns}
			if tt.mutate != nil {
				tt.mutate(result)
			}
			original := *result
			fallbackBefore := resumeRefused(!tt.cold, result)
			got := provenZeroCostRefusal(!tt.cold, result, stream)
			if got != tt.want {
				t.Fatalf("classification = %t, want %t", got, tt.want)
			}
			if resumeRefused(!tt.cold, result) != fallbackBefore {
				t.Fatal("classification changed existing fallback decision")
			}
			if result.Model != original.Model || result.Outcome != original.Outcome || result.ExitCode != original.ExitCode || result.Usage != nil {
				t.Fatal("classification changed model, outcome, exit status or invented token coverage")
			}
		})
	}
}

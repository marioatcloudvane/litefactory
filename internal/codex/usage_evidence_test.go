package codex

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// These envelopes and schedules are synthetic. They exercise exclusive response
// accounting, not authentic current-provider hierarchy compatibility.
func TestRolloutEvidence_ExclusiveResponses(t *testing.T) {
	t.Parallel()
	type step struct {
		payload    string
		historical bool
	}
	const own = `{"thread_id":"child","turn_id":"turn","response_id":"new","usage":{"input_tokens":20,"cached_input_tokens":8,"output_tokens":3,"reasoning_output_tokens":2}}`
	cases := []struct {
		name    string
		context string
		steps   []step
		count   int
		diag    string
		unknown bool
	}{
		{name: "own response/charged once", steps: []step{{own, false}, {own, false}}, count: 1},
		{name: "resume baseline/not charged again", steps: []step{{own, true}, {own, false}}, count: 0},
		{name: "resumed new response/only delta", steps: []step{{`{"thread_id":"child","turn_id":"turn","response_id":"old","usage":{"input_tokens":99}}`, true}, {own, false}}, count: 1},
		{name: "inherited parent/excluded", steps: []step{{`{"thread_id":"root","turn_id":"turn","response_id":"new","usage":{"input_tokens":20}}`, false}}, diag: "inherited_usage_excluded"},
		{name: "foreign sibling/excluded", steps: []step{{`{"thread_id":"sibling","turn_id":"turn","response_id":"new","usage":{"input_tokens":20}}`, false}}, diag: "inherited_usage_excluded"},
		{name: "negative counters/unresolved", steps: []step{{`{"thread_id":"child","turn_id":"turn","response_id":"new","usage":{"input_tokens":-1}}`, false}}, diag: "invalid_usage_counters"},
		{name: "cache exceeds input/unresolved", steps: []step{{`{"thread_id":"child","turn_id":"turn","response_id":"new","usage":{"input_tokens":1,"cached_input_tokens":2}}`, false}}, diag: "invalid_usage_counters"},
		{name: "malformed record/unresolved", steps: []step{{`{`, false}}, diag: "malformed_usage_event"},
		{name: "missing response identity/unresolved", steps: []step{{`{"thread_id":"child","turn_id":"turn","usage":{}}`, false}}, diag: "malformed_usage_event"},
		{name: "conflicting response/retain one and diagnose", steps: []step{{own, false}, {`{"thread_id":"child","turn_id":"turn","response_id":"new","usage":{"input_tokens":99}}`, false}}, count: 1, diag: "conflicting_response_identity"},
		{name: "foreign model turn/unknown model", context: `{"model":"gpt-6-astra","turn_id":"other"}`, steps: []step{{own, false}}, count: 1, diag: "model_attribution_unavailable", unknown: true},
		{name: "malformed model context/unknown model", context: `{`, steps: []step{{own, false}}, count: 1, diag: "model_attribution_unavailable", unknown: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []agent.UsageObservation
			c := newUsageCollector(t.Context(), agent.Request{Usage: &agent.UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e agent.UsageEvent) error {
				if e.Observation != nil {
					got = append(got, e.Observation.Clone())
				}
				return nil
			}}})
			c.rollouts = &rolloutUsage{}
			state := &rolloutFile{id: "child", version: "synthetic"}
			ctx := tt.context
			if ctx == "" {
				ctx = `{"model":"gpt-6-astra","turn_id":"turn"}`
			}
			c.rolloutRecord(state, rolloutRecord{Type: "turn_context", Payload: json.RawMessage(ctx)}, false)
			for _, s := range tt.steps {
				c.rolloutRecord(state, rolloutRecord{Type: "token_usage_record", Payload: json.RawMessage(s.payload)}, s.historical)
			}
			if len(got) != tt.count {
				t.Fatalf("observations=%d want %d", len(got), tt.count)
			}
			if tt.diag != "" && !slices.Contains(c.coverage.Diagnostics, tt.diag) {
				t.Fatalf("diagnostics=%v missing %s", c.coverage.Diagnostics, tt.diag)
			}
			for _, o := range got {
				if o.InvocationID != "child" || o.AttemptID != "attempt" || o.Mode != agent.CounterIncremental || o.Scope != agent.UsageExclusive {
					t.Fatalf("incorrect ownership or counter scope: %+v", o)
				}
				if o.Tokens.InputUncached == nil || *o.Tokens.InputUncached != 12 || o.Tokens.CacheRead == nil || *o.Tokens.CacheRead != 8 || o.Tokens.Output == nil || *o.Tokens.Output != 3 || o.Tokens.Reasoning == nil || *o.Tokens.Reasoning != 2 {
					t.Fatalf("wrong exclusive tokens: %+v", o.Tokens)
				}
				if tt.unknown {
					if o.ActualModel != nil {
						t.Fatal("invented model")
					}
				} else if o.ActualModel == nil || *o.ActualModel != "gpt-6-astra" {
					t.Fatal("lost runtime model")
				}
				if o.Coverage.Status == agent.CollectionComplete {
					t.Fatal("synthetic evidence certified completeness")
				}
			}
		})
	}
}

func TestRolloutEvidence_ResponseLimit(t *testing.T) {
	t.Parallel()
	var count int
	c := newUsageCollector(t.Context(), agent.Request{Usage: &agent.UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Observation != nil {
			count++
		}
		return nil
	}}})
	c.rollouts = &rolloutUsage{responses: 100000}
	c.rolloutRecord(&rolloutFile{id: "child"}, rolloutRecord{Type: "token_usage_record", Payload: json.RawMessage(`{"thread_id":"child","turn_id":"turn","response_id":"new","usage":{"input_tokens":20}}`)}, false)
	if count != 0 || !slices.Contains(c.coverage.Diagnostics, "usage_identity_limit") {
		t.Fatalf("count=%d diagnostics=%v", count, c.coverage.Diagnostics)
	}
}

// Implement and Plan use this same sink and parser; the mode is only metadata.
func TestRolloutEvidence_ImplementParallelNested(t *testing.T) {
	t.Parallel()
	var invocations []agent.UsageInvocation
	var observations []agent.UsageObservation
	c := newUsageCollector(t.Context(), agent.Request{Mode: "implement", Usage: &agent.UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Invocation != nil {
			invocations = append(invocations, *e.Invocation)
		}
		if e.Observation != nil {
			observations = append(observations, e.Observation.Clone())
		}
		return nil
	}}})
	c.observe([]byte(`{"type":"thread.started","thread_id":"root"}`))
	c.observe([]byte(`{"type":"item.completed","item":{"type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"root","receiver_thread_ids":["child-a","child-b"]}}`))
	c.observe([]byte(`{"type":"item.completed","item":{"type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"child-a","receiver_thread_ids":["grandchild"]}}`))
	c.rollouts = &rolloutUsage{}
	for _, id := range []string{"child-a", "child-b", "grandchild"} {
		state := &rolloutFile{id: id}
		c.rolloutRecord(state, rolloutRecord{Type: "turn_context", Payload: json.RawMessage(`{"model":"gpt-6-astra","turn_id":"turn"}`)}, false)
		payload, _ := json.Marshal(map[string]any{"thread_id": id, "turn_id": "turn", "response_id": "response", "usage": map[string]int{"input_tokens": 10, "cached_input_tokens": 4, "output_tokens": 2}})
		for range 2 {
			c.rolloutRecord(state, rolloutRecord{Type: "token_usage_record", Payload: payload}, false)
		}
	}
	if len(invocations) != 4 || len(observations) != 3 {
		t.Fatalf("invocations=%d observations=%d", len(invocations), len(observations))
	}
	parents := map[string]string{"root": "", "child-a": "root", "child-b": "root", "grandchild": "child-a"}
	for _, inv := range invocations {
		if want, ok := parents[inv.ID]; !ok || inv.ParentID != want {
			t.Fatalf("wrong ancestry %+v", inv)
		}
	}
	var input, output int64
	for _, o := range observations {
		input += *o.Tokens.InputUncached
		output += *o.Tokens.Output
	}
	if input != 18 || output != 6 {
		t.Fatalf("exclusive totals input=%d output=%d", input, output)
	}
}

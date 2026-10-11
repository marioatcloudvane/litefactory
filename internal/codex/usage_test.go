package codex

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Synthetic JSONL checks decoding only, not authentic provider compatibility.
func TestUsageSyntheticTopologyAndLifetimeSummary(t *testing.T) {
	t.Parallel()
	var events []agent.UsageEvent
	c := newUsageCollector(t.Context(), agent.Request{Usage: &agent.UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e agent.UsageEvent) error { events = append(events, e); return nil }}})
	input := `{"type":"thread.started","thread_id":"root"}
{"type":"item.completed","item":{"type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"root","receiver_thread_ids":["child"]}}
{"type":"item.completed","item":{"type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"child","receiver_thread_ids":["grandchild"]}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":3,"reasoning_output_tokens":2}}
`
	state := consumeUsage(strings.NewReader(input), io.Discard, c)
	coverage := c.finish()
	if !state.completed || coverage.Status != agent.CollectionPartial {
		t.Fatalf("state=%+v coverage=%+v", state, coverage)
	}
	if c.parents["grandchild"] != "child" {
		t.Fatal("missing ancestry")
	}
	var observations int
	for _, e := range events {
		if o := e.Observation; o != nil {
			observations++
			if o.ActualModel != nil || o.Scope != agent.UsageInclusive || *o.Tokens.InputUncached != 6 || *o.Tokens.Reasoning != 2 {
				t.Fatalf("unsafe summary %+v", o)
			}
		}
	}
	if observations != 1 {
		t.Fatalf("observations %d", observations)
	}
}

func TestUsageSyntheticMalformedDoesNotChangeOutcome(t *testing.T) {
	t.Parallel()
	c := newUsageCollector(t.Context(), agent.Request{Usage: &agent.UsageCapture{AttemptID: "a", Sink: func(context.Context, agent.UsageEvent) error { return nil }}})
	s := consumeUsage(strings.NewReader("{\"type\":\"thread.started\",\"thread_id\":\"r\"}\n{\"type\":\"turn.failed\"}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":-1}}\n"), io.Discard, c)
	if !s.failed || !s.completed {
		t.Fatal("collector changed terminal classification")
	}
	if c.finish().Status == agent.CollectionComplete {
		t.Fatal("unproven complete coverage")
	}
}

func TestUsageFailedTurnIsNotComplete(t *testing.T) {
	t.Parallel()
	c := newUsageCollector(t.Context(), agent.Request{Usage: &agent.UsageCapture{AttemptID: "a", Sink: func(context.Context, agent.UsageEvent) error { return nil }}})
	consumeUsage(strings.NewReader("{\"type\":\"thread.started\",\"thread_id\":\"r\"}\n{\"type\":\"turn.failed\"}\n"), io.Discard, c)
	got := c.finish()
	if got.Status == agent.CollectionComplete || !slices.Contains(got.Diagnostics, "failed_turn_usage_unavailable") {
		t.Fatalf("coverage %+v", got)
	}
}

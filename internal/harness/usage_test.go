package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// These fixtures exercise collector mechanics; they are synthetic and do not
// certify any provider version or the availability of complete child streams.
func TestUsageSyntheticHierarchyReplay(t *testing.T) {
	t.Parallel()
	var events []agent.UsageEvent
	req := agent.Request{Session: agent.SessionRef{Ref: "session"}, Usage: &agent.UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e agent.UsageEvent) error { events = append(events, e); return nil }}}
	c := newUsageCollector(t.Context(), req)
	child := []byte(`{"type":"assistant","session_id":"session","parent_tool_use_id":"child","message":{"id":"m2","model":"child-model","usage":{"input_tokens":5,"output_tokens":2}}}`)
	c.observe(child) // arrives before root's tool registration
	root := []byte(`{"type":"assistant","session_id":"session","message":{"id":"m1","model":"root-model","content":[{"type":"tool_use","name":"Agent","id":"child"}],"usage":{"input_tokens":10,"output_tokens":3}}}`)
	c.observe(root)
	c.observe(child)
	c.observe(root)
	c.observe([]byte(`{"type":"result","session_id":"session","modelUsage":{"root-model":{"inputTokens":10,"outputTokens":3}}}`))
	coverage := c.finish()
	if coverage.Status != agent.CollectionPartial {
		t.Fatalf("coverage %v", coverage)
	}
	var exclusive, inclusive int
	for _, e := range events {
		if e.Observation != nil {
			if e.Observation.Scope == agent.UsageExclusive {
				exclusive++
			} else {
				inclusive++
			}
		}
	}
	if exclusive != 2 || inclusive != 1 {
		t.Fatalf("exclusive=%d inclusive=%d", exclusive, inclusive)
	}
	if c.invocations["tool:child"] != "root" {
		t.Fatal("missing ancestry")
	}
}

func TestUsageSyntheticInvalidAndSinkFailure(t *testing.T) {
	t.Parallel()
	c := newUsageCollector(t.Context(), agent.Request{Session: agent.SessionRef{Ref: "session"}, Usage: &agent.UsageCapture{AttemptID: "a", Sink: func(context.Context, agent.UsageEvent) error { return errors.New("database unavailable") }}})
	c.observe([]byte(`{"type":"assistant","message":{"id":"m","model":"model","usage":{"input_tokens":-1}}}`))
	got := c.finish()
	if got.Status == agent.CollectionComplete || !c.failedSink {
		t.Fatalf("coverage %v", got)
	}
}

func TestUsageSyntheticResumeCannotChargeHistory(t *testing.T) {
	t.Parallel()
	var observations []agent.UsageObservation
	c := newUsageCollector(t.Context(), agent.Request{Path: agent.PathResume, Session: agent.SessionRef{Ref: "s"}, Usage: &agent.UsageCapture{AttemptID: "a", Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Observation != nil {
			observations = append(observations, *e.Observation)
		}
		return nil
	}}})
	c.observe([]byte(`{"type":"assistant","message":{"id":"old","model":"m","usage":{"input_tokens":10,"output_tokens":3}}}`))
	if len(observations) != 1 || observations[0].Scope != agent.UsageInclusive {
		t.Fatalf("history could become additive: %+v", observations)
	}
}

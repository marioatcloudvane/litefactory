package claude

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

// These fixtures exercise collector mechanics; they are synthetic and do not
// certify any provider version or the availability of complete child streams.
func TestUsageSyntheticHierarchyReplay(t *testing.T) {
	t.Parallel()
	var events []agent.UsageEvent
	req := testUsageRequest{Session: testSession{Ref: "session"}, Usage: &agent.UsageCapture{Sink: func(_ context.Context, e agent.UsageEvent) error { events = append(events, e); return nil }}}
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
	c := newUsageCollector(t.Context(), testUsageRequest{Session: testSession{Ref: "session"}, Usage: &agent.UsageCapture{Sink: func(context.Context, agent.UsageEvent) error { return errors.New("database unavailable") }}})
	c.observe([]byte(`{"type":"assistant","message":{"id":"m","model":"model","usage":{"input_tokens":-1}}}`))
	got := c.finish()
	if got.Status == agent.CollectionComplete || !c.failedSink {
		t.Fatalf("coverage %v", got)
	}
}

func TestUsageSyntheticResumeCannotChargeHistory(t *testing.T) {
	t.Parallel()
	var observations []agent.UsageObservation
	c := newUsageCollector(t.Context(), testUsageRequest{Path: true, Session: testSession{Ref: "s"}, Usage: &agent.UsageCapture{Sink: func(_ context.Context, e agent.UsageEvent) error {
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

// completedClaudeStream is a synthetic completed run with one Agent subagent.
// Its result summary matches the summed exclusive totals per model.
const completedClaudeStream = `{"type":"system","subtype":"init","session_id":"session","claude_code_version":"fixture"}
{"type":"assistant","session_id":"session","message":{"id":"m1","model":"root-model","content":[{"type":"tool_use","name":"Agent","id":"child"}],"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}}
{"type":"assistant","session_id":"session","parent_tool_use_id":"child","message":{"id":"m2","model":"child-model","usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}}
{"type":"user","session_id":"session","message":{"content":[{"type":"tool_result","tool_use_id":"child"}]}}
{"type":"assistant","session_id":"session","message":{"id":"m3","model":"root-model","usage":{"input_tokens":4,"output_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}}
{"type":"result","session_id":"session","subtype":"success","modelUsage":{"root-model":{"inputTokens":14,"outputTokens":4,"cacheReadInputTokens":4,"cacheCreationInputTokens":2},"child-model":{"inputTokens":5,"outputTokens":2,"cacheReadInputTokens":2,"cacheCreationInputTokens":1}}}`

func TestUsageCompletedRunWithSubagentIsComplete(t *testing.T) {
	t.Parallel()
	var events []agent.UsageEvent
	c := newUsageCollector(t.Context(), testUsageRequest{Session: testSession{Ref: "session"}, Usage: &agent.UsageCapture{Sink: func(_ context.Context, e agent.UsageEvent) error { events = append(events, e); return nil }}})
	for line := range strings.SplitSeq(completedClaudeStream, "\n") {
		c.observe([]byte(line))
	}
	got := c.finish()
	if got.Status != agent.CollectionComplete || len(got.Diagnostics) != 0 {
		t.Fatalf("coverage %+v", got)
	}
	var input, output int64
	for _, e := range events {
		if o := e.Observation; o != nil && o.Scope == agent.UsageExclusive {
			input += *o.Tokens.InputUncached
			output += *o.Tokens.Output
		}
	}
	if input != 19 || output != 6 {
		t.Fatalf("exclusive totals input=%d output=%d", input, output)
	}
}

func TestUsageInterruptedRunIsNotComplete(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	c := newUsageCollector(ctx, testUsageRequest{Session: testSession{Ref: "session"}, Usage: &agent.UsageCapture{Sink: func(context.Context, agent.UsageEvent) error { return nil }}})
	lines := strings.Split(completedClaudeStream, "\n")
	for _, line := range lines[:len(lines)-1] {
		c.observe([]byte(line))
	}
	cancel()
	got := c.finish()
	if got.Status == agent.CollectionComplete || !slices.Contains(got.Diagnostics, "missing_terminal_usage") || !slices.Contains(got.Diagnostics, "collection_cancelled") {
		t.Fatalf("coverage %+v", got)
	}
}

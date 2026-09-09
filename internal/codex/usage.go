package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
)

// usageCollector consumes the existing exec JSONL transport only. The upstream
// exec processor emits lifetime total usage, despite the event's turn name.
// Without a verified initial baseline and model evidence it is reconciliation
// evidence only; never silently charge a resumed conversation again.
type usageCollector struct {
	ctx        context.Context
	capture    agent.UsageCapture
	thread     string
	parents    map[string]string
	coverage   agent.UsageCoverage
	sinkFailed bool
	terminal   bool
}

func newUsageCollector(ctx context.Context, req agent.Request) *usageCollector {
	if req.Usage == nil {
		return nil
	}
	return &usageCollector{ctx: ctx, capture: *req.Usage, parents: make(map[string]string), coverage: agent.UsageCoverage{Status: agent.CollectionUnknown, SourceFormat: "exec-jsonl", Diagnostics: []string{"capability_evidence_missing", "model_attribution_unavailable", "usage_baseline_unverified"}}}
}
func (c *usageCollector) diagnostic(code string) {
	if !slices.Contains(c.coverage.Diagnostics, code) {
		c.coverage.Diagnostics = append(c.coverage.Diagnostics, code)
		slices.Sort(c.coverage.Diagnostics)
	}
}
func (c *usageCollector) emit(e agent.UsageEvent) {
	if c.sinkFailed {
		return
	}
	if err := c.capture.Emit(c.ctx, e); err != nil {
		c.sinkFailed = true
		c.diagnostic("usage_sink_failed")
	}
}
func validUsageID(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\n\r\t")
}
func (c *usageCollector) invocation(id, parent string) {
	if previous, ok := c.parents[id]; ok {
		if previous != parent {
			c.diagnostic("conflicting_parent")
		}
		return
	}
	if len(c.parents) >= 100000 {
		c.diagnostic("usage_identity_limit")
		return
	}
	c.parents[id] = parent
	c.emit(agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: c.capture.AttemptID, ID: id, ParentID: parent, SourceID: c.thread, SourceInvocationID: id, Coverage: c.coverage.Clone()}})
}
func (c *usageCollector) observe(line []byte) {
	if c == nil {
		return
	}
	var event struct {
		Type   string `json:"type"`
		Thread string `json:"thread_id"`
		Usage  *struct {
			Input     *int64 `json:"input_tokens"`
			Read      *int64 `json:"cached_input_tokens"`
			Write     *int64 `json:"cache_write_input_tokens"`
			Output    *int64 `json:"output_tokens"`
			Reasoning *int64 `json:"reasoning_output_tokens"`
		} `json:"usage"`
		Item struct {
			Type      string   `json:"type"`
			Tool      string   `json:"tool"`
			Sender    string   `json:"sender_thread_id"`
			Receivers []string `json:"receiver_thread_ids"`
		} `json:"item"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		c.diagnostic("malformed_usage_event")
		return
	}
	if event.Type == "thread.started" {
		if !validUsageID(event.Thread) {
			c.diagnostic("missing_thread_identity")
			return
		}
		if c.thread != "" && c.thread != event.Thread {
			c.diagnostic("conflicting_thread_identity")
			return
		}
		c.thread = event.Thread
		c.invocation(c.thread, "")
		return
	}
	if c.thread == "" {
		c.diagnostic("missing_thread_identity")
		return
	}
	if event.Item.Type == "collab_tool_call" && event.Item.Tool == "spawn_agent" {
		if _, ok := c.parents[event.Item.Sender]; !ok {
			c.diagnostic("unresolved_parent")
			return
		}
		if len(event.Item.Receivers) > 10000 {
			c.diagnostic("usage_identity_limit")
			return
		}
		for _, child := range event.Item.Receivers {
			if validUsageID(child) && child != event.Item.Sender && child != c.thread {
				c.invocation(child, event.Item.Sender)
			} else {
				c.diagnostic("invalid_child_identity")
			}
		}
		c.diagnostic("descendant_usage_unavailable")
	}
	if event.Type == "turn.failed" {
		c.terminal = true
		c.diagnostic("failed_turn_usage_unavailable")
	}
	if event.Type != "turn.completed" {
		return
	}
	c.terminal = true
	if event.Usage == nil {
		c.diagnostic("missing_terminal_usage")
		return
	}
	u := event.Usage
	tokens, err := meter.Normalize(agent.TokenUsage{InputUncached: u.Input, CacheRead: u.Read, CacheWrite: u.Write, Output: u.Output, Reasoning: u.Reasoning}, true)
	if err != nil {
		c.diagnostic("invalid_usage_counters")
		return
	}
	c.coverage.Status = agent.CollectionPartial
	encoded, _ := json.Marshal(tokens)
	digest := sha256.Sum256(encoded)
	c.emit(agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: c.capture.AttemptID, SourceID: c.thread, EventKey: "total:" + hex.EncodeToString(digest[:]), InvocationID: c.thread, ModelProvenance: "unknown", Mode: agent.CounterIncremental, Scope: agent.UsageInclusive, Tokens: tokens, Coverage: c.coverage.Clone()}})
}
func (c *usageCollector) finish() *agent.UsageCoverage {
	if c == nil {
		return nil
	}
	if !c.terminal {
		c.diagnostic("missing_terminal_usage")
	}
	if c.ctx.Err() != nil {
		c.diagnostic("collection_cancelled")
	}
	c.emit(agent.UsageEvent{Coverage: &c.coverage})
	coverage := c.coverage.Clone()
	return &coverage
}

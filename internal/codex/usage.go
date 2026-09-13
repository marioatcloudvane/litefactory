package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
)

// usageCollector retains exec totals as reconciliation evidence and collects
// exclusive response records from associated rollouts. Runtime model context
// and prelaunch byte boundaries prevent configured-model guesses and resume
// recharging. Provider certification remains separate from observed subtotals.
type usageCollector struct {
	ctx        context.Context
	capture    agent.UsageCapture
	thread     string
	parents    map[string]string
	coverage   agent.UsageCoverage
	sinkFailed bool
	terminal   bool
	rollouts   *rolloutUsage
	window     *HistoricalWindow
}

func newUsageCollector(ctx context.Context, req agent.Request) *usageCollector {
	if req.Usage == nil {
		return nil
	}
	return &usageCollector{ctx: ctx, capture: *req.Usage, parents: make(map[string]string), coverage: agent.UsageCoverage{Status: agent.CollectionUnknown, SourceFormat: "exec-jsonl", Diagnostics: []string{"capability_evidence_missing"}}}
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
	defer c.collectRollouts(false)
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
	cancelled := c.ctx.Err() != nil
	if cancelled {
		c.diagnostic("collection_cancelled")
	}
	// After process exit, allow at most two seconds to drain already persisted
	// records. This is bounded finalization, never a new provider execution.
	drain, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), 2*time.Second)
	defer cancel()
	previousContext := c.ctx
	c.ctx = drain
	defer func() { c.ctx = previousContext }()
	// Bound delayed child flush collection by the drain deadline. This timer
	// belongs to finalization only, not subprocess retry scheduling.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for drain.Err() == nil {
		c.collectRollouts(true)
		if c.rollouts == nil || c.rollouts.disabled || drain.Err() != nil {
			break
		}
		select {
		case <-drain.Done():
		case <-ticker.C:
		}
	}
	// The drain deadline is not request cancellation. Restore the request
	// context before diagnostics and the final sink write.
	c.ctx = previousContext
	c.diagnoseRollouts()
	if c.rollouts == nil {
		c.diagnostic("usage_source_unavailable")
	}
	if c.rollouts != nil && c.rollouts.root != nil {
		if err := c.rollouts.root.Close(); err != nil {
			c.diagnostic("usage_source_close_failed")
		}
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

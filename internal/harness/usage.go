package harness

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

// usageCollector combines the process-owned stream with its associated session
// transcripts. Configured models are never used as actual-model evidence.
// Until external captures establish completeness, every total is qualified.
type usageCollector struct {
	ctx             context.Context
	capture         agent.UsageCapture
	session         string
	coverage        agent.UsageCoverage
	invocations     map[string]string
	messages        map[string]agent.TokenUsage
	seen            map[string]bool
	baselineSeen    map[string]bool
	pending         [][]byte
	pendingBytes    int
	resumed         bool
	baselineReady   bool
	baselineInvalid bool
	baselining      bool
	transcripts     *usageTranscripts
	agents          map[string]string
	measured        map[string]bool
	completed       map[string]bool
	totals          map[string]agent.TokenUsage
	summaries       map[string]agent.TokenUsage
	terminal        bool
	observed        bool
	failedSink      bool
}

const maxUsageIdentities = 100000

type messageUsage struct {
	Input    *int64 `json:"input_tokens"`
	Output   *int64 `json:"output_tokens"`
	Read     *int64 `json:"cache_read_input_tokens"`
	Write    *int64 `json:"cache_creation_input_tokens"`
	Creation struct {
		Five *int64 `json:"ephemeral_5m_input_tokens"`
		Hour *int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (m messageUsage) tokens() agent.TokenUsage {
	return agent.TokenUsage{InputUncached: m.Input, Output: m.Output, CacheRead: m.Read, CacheWrite: m.Write, CacheWrite5m: m.Creation.Five, CacheWrite1h: m.Creation.Hour}
}

func newUsageCollector(ctx context.Context, req agent.Request) *usageCollector {
	if req.Usage == nil {
		return nil
	}
	collector := &usageCollector{ctx: ctx, capture: *req.Usage, session: req.Session.Ref,
		coverage:    agent.UsageCoverage{Status: agent.CollectionUnknown, SourceFormat: "stream-json", Diagnostics: []string{"capability_evidence_missing"}},
		invocations: make(map[string]string), messages: make(map[string]agent.TokenUsage), seen: make(map[string]bool), agents: make(map[string]string), measured: make(map[string]bool), completed: make(map[string]bool), totals: make(map[string]agent.TokenUsage), summaries: make(map[string]agent.TokenUsage)}
	if req.Path.Resuming() {
		collector.resumed = true
	}
	return collector
}

func (c *usageCollector) diagnostic(code string) {
	if c.baselining && (strings.Contains(code, "invalid") || strings.Contains(code, "malformed") || strings.Contains(code, "regress") || strings.Contains(code, "limit") || strings.Contains(code, "unassociated") || strings.Contains(code, "unsafe") || strings.Contains(code, "incomplete")) {
		c.baselineInvalid = true
	}

	if !slices.Contains(c.coverage.Diagnostics, code) {
		c.coverage.Diagnostics = append(c.coverage.Diagnostics, code)
		slices.Sort(c.coverage.Diagnostics)
	}
}

func (c *usageCollector) emit(e agent.UsageEvent) {
	if c.baselining || c.failedSink {
		return
	}
	if err := c.capture.Emit(c.ctx, e); err != nil {
		c.failedSink = true
		c.diagnostic("usage_sink_failed")
	}
}

func usageIdentity(s string) bool {
	return s != "" && len(s) <= 256 && !strings.ContainsAny(s, "\x00\r\n\t")
}

func (c *usageCollector) invocation(id, parent string) bool {
	if old, ok := c.invocations[id]; ok {
		if old != parent {
			c.diagnostic("conflicting_parent")
			return false
		}
		return true
	}
	if len(c.invocations) >= maxUsageIdentities {
		c.diagnostic("usage_identity_limit")
		return false
	}
	c.invocations[id] = parent
	c.emit(agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: c.capture.AttemptID, ID: id, ParentID: parent, SourceID: c.session, SourceInvocationID: id, Coverage: c.coverage.Clone()}})
	return true
}

func (c *usageCollector) observe(line []byte) {
	if c == nil {
		return
	}
	c.process(line)
	for {
		before := len(c.invocations)
		pending := c.pending
		c.pending = nil
		c.pendingBytes = 0
		for _, record := range pending {
			c.process(record)
		}
		if len(c.invocations) == before {
			break
		}
	}
}

func (c *usageCollector) process(line []byte) {
	if c == nil {
		return
	}
	var e struct {
		Type       string          `json:"type"`
		Subtype    string          `json:"subtype"`
		Session    string          `json:"session_id"`
		Parent     string          `json:"parent_tool_use_id"`
		UUID       string          `json:"uuid"`
		Version    string          `json:"claude_code_version"`
		ToolResult json.RawMessage `json:"tool_use_result"`
		Message    struct {
			ID      string          `json:"id"`
			Model   string          `json:"model"`
			Usage   *messageUsage   `json:"usage"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		c.diagnostic("malformed_usage_event")
		return
	}
	var content []struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		ID        string `json:"id"`
		ToolUseID string `json:"tool_use_id"`
		IsError   bool   `json:"is_error"`
	}
	if len(e.Message.Content) > 0 && e.Message.Content[0] == '[' {
		if json.Unmarshal(e.Message.Content, &content) != nil {
			c.diagnostic("malformed_usage_event")
			return
		}
	}
	var toolResult struct {
		AgentID string `json:"agentId"`
	}
	if len(e.ToolResult) > 0 && e.ToolResult[0] == '{' {
		if json.Unmarshal(e.ToolResult, &toolResult) != nil {
			c.diagnostic("malformed_tool_result")
			return
		}
	}
	if !usageIdentity(c.session) {
		c.diagnostic("missing_session_identity")
		return
	}
	if e.Session != "" && e.Session != c.session {
		c.diagnostic("unassociated_session")
		return
	}
	// Replayed history is filtered before creating attempt-local ancestry rows.
	if e.Type == "assistant" && e.Message.Usage != nil {
		model := e.Message.Model
		if !usageIdentity(model) {
			model = ""
		}
		identity, _ := json.Marshal([]string{e.Message.ID, model})
		encoded, _ := json.Marshal(e.Message.Usage.tokens())
		digest := sha256.Sum256(encoded)
		if !c.baselining && c.baselineSeen[string(identity)+":"+hex.EncodeToString(digest[:])] {
			return
		}
	}
	if !c.invocation("root", "") {
		return
	}
	if usageIdentity(e.Version) {
		c.coverage.Version = e.Version
	}
	if e.Type == "result" {
		c.terminal = true
		c.summary(line)
		return
	}
	if e.Type != "assistant" && e.Type != "user" {
		return
	}
	invocation := "root"
	if e.Parent != "" {
		if !usageIdentity(e.Parent) {
			c.diagnostic("invalid_parent_identity")
			return
		}
		invocation = "tool:" + e.Parent
		if _, ok := c.invocations[invocation]; !ok && !c.restoreInvocation(invocation, 0) {
			if c.pendingBytes+len(line) > 8<<20 {
				c.diagnostic("pending_usage_limit")
				return
			}
			c.pending = append(c.pending, slices.Clone(line))
			c.pendingBytes += len(line)
			return
		}
	}
	if e.Type == "user" {
		for _, part := range content {
			if part.Type == "tool_result" {
				if _, ok := c.invocations["tool:"+part.ToolUseID]; ok {
					c.completed["tool:"+part.ToolUseID] = true
				}
			}
			if part.Type == "tool_result" && part.IsError {
				if _, ok := c.invocations["tool:"+part.ToolUseID]; ok {
					c.diagnostic("descendant_failed")
				}
			}
			if part.Type == "tool_result" && usageIdentity(part.ToolUseID) && transcriptAgentID(toolResult.AgentID) {
				c.associateAgent(toolResult.AgentID, "tool:"+part.ToolUseID, invocation)
			}
		}
		return
	}
	for _, part := range content {
		if part.Type == "tool_use" && (part.Name == "Agent" || part.Name == "Task") && usageIdentity(part.ID) {
			c.invocation("tool:"+part.ID, invocation)
		}
	}
	if e.Message.Usage == nil {
		return
	}
	if !usageIdentity(e.Message.ID) {
		c.diagnostic("missing_message_identity")
		return
	}
	tokens := e.Message.Usage.tokens()
	if err := meter.Validate(tokens); err != nil {
		c.diagnostic("invalid_usage_counters")
		return
	}
	// Usage can be repeated on separate content blocks of the same message.
	// Delta only within this observed message; no assumed lifetime baseline.
	modelID := e.Message.Model
	if !usageIdentity(modelID) {
		modelID = ""
	}
	identity, _ := json.Marshal([]string{e.Message.ID, modelID})
	key := string(identity)
	encoded, _ := json.Marshal(tokens)
	digest := sha256.Sum256(encoded)
	eventKey := key + ":" + hex.EncodeToString(digest[:])
	if c.seen[eventKey] {
		return
	}
	if len(c.seen) >= maxUsageIdentities {
		c.diagnostic("usage_identity_limit")
		return
	}
	c.seen[eventKey] = true
	delta := tokens
	if prior, ok := c.messages[key]; ok {
		var err error
		delta, err = meter.Delta(tokens, prior)
		if err != nil {
			c.diagnostic("regressing_message_usage")
			return
		}
	}
	c.messages[key] = tokens.Clone()
	c.measured[invocation] = true
	c.observed = true
	c.coverage.Status = agent.CollectionPartial
	var model *string
	provenance := "unknown"
	if usageIdentity(e.Message.Model) {
		model = &e.Message.Model
		provenance = "assistant_message"
	} else {
		c.diagnostic("unknown_actual_model")
	}
	scope := agent.UsageExclusive
	if c.resumed && !c.baselineReady {
		scope = agent.UsageInclusive
	} // Unverified replay cannot become a new charge.
	if !c.baselining && scope == agent.UsageExclusive {
		if old, ok := c.totals[modelID]; ok {
			if sum, err := meter.Add(old, delta); err == nil {
				c.totals[modelID] = sum
			} else {
				c.diagnostic("invalid_usage_counters")
			}
		} else {
			c.totals[modelID] = delta.Clone()
		}
	}
	c.emit(agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: c.capture.AttemptID, SourceID: c.session, EventKey: eventKey, InvocationID: invocation, ActualModel: model, ModelProvenance: provenance, Mode: agent.CounterIncremental, Scope: scope, Tokens: delta, Coverage: c.coverage.Clone()}})
}

func (c *usageCollector) finish() *agent.UsageCoverage {
	if c == nil {
		return nil
	}
	c.finalizeTranscripts()
	for invocation := range c.invocations {
		if invocation != "root" && !c.completed[invocation] {
			c.diagnostic("descendant_finalization_incomplete")
		}
		if invocation != "root" && !c.measured[invocation] {
			c.diagnostic("missing_descendant_usage")
		}
	}
	for model, summary := range c.summaries {
		total, ok := c.totals[model]
		if !ok {
			c.diagnostic("inclusive_summary_mismatch")
			continue
		}
		if !sameSummary(total, summary) {
			c.diagnostic("inclusive_summary_mismatch")
		}
	}

	if c.resumed && !c.baselineReady {
		c.diagnostic("resume_baseline_unverified")
	}
	if len(c.pending) > 0 {
		c.diagnostic("unresolved_parent")
	}
	if !c.terminal {
		c.diagnostic("missing_terminal_usage")
	}
	if c.ctx.Err() != nil {
		c.diagnostic("collection_cancelled")
	}
	if c.observed {
		c.coverage.Status = agent.CollectionPartial
	}
	c.emit(agent.UsageEvent{Coverage: &c.coverage})
	result := c.coverage.Clone()
	return &result
}

// summary retains provider model totals strictly as non-additive evidence.
func (c *usageCollector) summary(line []byte) {
	var event struct {
		ModelUsage map[string]struct {
			Input  *int64 `json:"inputTokens"`
			Output *int64 `json:"outputTokens"`
			Read   *int64 `json:"cacheReadInputTokens"`
			Write  *int64 `json:"cacheCreationInputTokens"`
		} `json:"modelUsage"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		c.diagnostic("malformed_model_summary")
		return
	}
	if len(event.ModelUsage) > 1024 {
		c.diagnostic("usage_identity_limit")
		return
	}
	models := make([]string, 0, len(event.ModelUsage))
	for model := range event.ModelUsage {
		models = append(models, model)
	}
	slices.Sort(models)
	for _, model := range models {
		if !usageIdentity(model) {
			c.diagnostic("unknown_actual_model")
			continue
		}
		values := event.ModelUsage[model]
		tokens := agent.TokenUsage{InputUncached: values.Input, Output: values.Output, CacheRead: values.Read, CacheWrite: values.Write}
		if err := meter.Validate(tokens); err != nil {
			c.diagnostic("invalid_usage_counters")
			continue
		}
		c.summaries[model] = tokens.Clone()
		encoded, _ := json.Marshal(tokens)
		sum := sha256.Sum256(encoded)
		c.emit(agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: c.capture.AttemptID, SourceID: c.session, EventKey: "summary:" + model + ":" + hex.EncodeToString(sum[:]), ActualModel: &model, ModelProvenance: "result_model_usage", Mode: agent.CounterIncremental, Scope: agent.UsageInclusive, Tokens: tokens, Coverage: c.coverage.Clone()}})
	}
}

// A summary validates only dimensions which both sources measured. Missing
// dimensions cannot certify completeness, which remains capability-qualified.
func sameSummary(a, b agent.TokenUsage) bool {
	for _, pair := range [][2]*int64{{a.InputUncached, b.InputUncached}, {a.CacheRead, b.CacheRead}, {a.CacheWrite, b.CacheWrite}, {a.Output, b.Output}} {
		if pair[0] == nil || pair[1] == nil {
			return false
		}
		if *pair[0] != *pair[1] {
			return false
		}
	}
	return true
}

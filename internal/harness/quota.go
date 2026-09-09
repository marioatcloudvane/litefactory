package harness

import (
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

type quotaWindow struct {
	Utilization float64         `json:"utilization"`
	ResetsAt    json.RawMessage `json:"resetsAt"`
}

type quotaInfo struct {
	Status         string                 `json:"status"`
	ResetsAt       json.RawMessage        `json:"resetsAt"`
	RateLimitType  string                 `json:"rateLimitType"`
	UnifiedWindows map[string]quotaWindow `json:"unifiedWindows"`
}

// quotaEvidence accumulates only explicitly rejected windows. Repetition is
// idempotent; an unknown reset must not disappear behind another usable window
// within a rejection sequence. Later top-level admission supersedes that sequence.
type quotaEvidence struct {
	rejected bool
	invalid  bool
	latest   time.Time
	earliest time.Time
}

func (q *quotaEvidence) observe(info quotaInfo) {
	if info.Status == "allowed" || info.Status == "allowed_warning" {
		// Admission is provider evidence that the previous rejection no longer
		// blocks this invocation. An unrelated terminal error cannot revive it.
		*q = quotaEvidence{}
		return
	}
	if info.Status != "rejected" {
		return
	}
	q.rejected = true
	q.add(info.ResetsAt)
	for name, window := range info.UnifiedWindows {
		if window.Utilization >= 1 || name == info.RateLimitType {
			q.add(window.ResetsAt)
		}
	}
}

func (q *quotaEvidence) add(raw json.RawMessage) {
	var epoch int64
	if json.Unmarshal(raw, &epoch) != nil || epoch <= 0 || epoch > 253402300799 {
		q.invalid = true
		return
	}
	reset := time.Unix(epoch, 0).UTC()
	if reset.After(q.latest) {
		q.latest = reset
	}
	if q.earliest.IsZero() || reset.Before(q.earliest) {
		q.earliest = reset
	}
}

func (q quotaEvidence) interruption(result *resultEvent, observed time.Time) *agent.Interruption {
	if !q.rejected || result == nil || !result.IsError {
		return nil
	}
	out := &agent.Interruption{Precision: agent.ResetExact}
	if q.invalid || q.earliest.IsZero() || !q.earliest.After(observed) {
		out.Reason = "an exhausted quota window has no usable future reset"
		return out
	}
	out.ResetBound, out.DueAt = q.latest, q.latest.Add(time.Minute)
	return out
}

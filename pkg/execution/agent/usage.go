package agent

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// TokenUsage contains exclusive token dimensions. Nil means unknown, including
// an unknown counter baseline; zero is a measured zero. Cache duration buckets
// are subsets of CacheWrite, and Reasoning is a subset of Output.
type TokenUsage struct {
	InputUncached *int64 `json:"input_uncached"`
	CacheRead     *int64 `json:"cache_read"`
	CacheWrite    *int64 `json:"cache_write"`
	CacheWrite5m  *int64 `json:"cache_write_5m"`
	CacheWrite1h  *int64 `json:"cache_write_1h"`
	Output        *int64 `json:"output"`
	Reasoning     *int64 `json:"reasoning"`
}

// Clone transfers ownership of all optional counters.
func (u TokenUsage) Clone() TokenUsage {
	return TokenUsage{copyCounter(u.InputUncached), copyCounter(u.CacheRead), copyCounter(u.CacheWrite), copyCounter(u.CacheWrite5m), copyCounter(u.CacheWrite1h), copyCounter(u.Output), copyCounter(u.Reasoning)}
}

func copyCounter(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// CollectionStatus qualifies measured subtotals independently of process success.
type CollectionStatus string

// Collection states distinguish absent evidence, known subtotals with gaps,
// and complete collection independently of whether the subprocess succeeded.
const (
	CollectionUnknown  CollectionStatus = "unknown"
	CollectionPartial  CollectionStatus = "partial"
	CollectionComplete CollectionStatus = "complete"
)

// UsageCoverage contains controlled diagnostic codes, never provider prose.
// Version and SourceFormat identify the evidence contract used by an adapter.
type UsageCoverage struct {
	Status       CollectionStatus `json:"status"`
	Diagnostics  []string         `json:"diagnostics"`
	Version      string           `json:"version"`
	SourceFormat string           `json:"source_format"`
}

// Clone prevents concurrent reporting from mutating adapter diagnostics.
func (c UsageCoverage) Clone() UsageCoverage { c.Diagnostics = slices.Clone(c.Diagnostics); return c }

// UsageInvocation identifies a root or descendant within an invocation tree.
// ParentID is empty only for a root; unresolved associations are incomplete.
type UsageInvocation struct {
	ID                 string        `json:"id"`
	ParentID           string        `json:"parent_id"`
	SourceID           string        `json:"source_id"`
	SourceInvocationID string        `json:"source_invocation_id"`
	Coverage           UsageCoverage `json:"coverage"`
}

// CounterMode describes whether an observation adds tokens or replaces a
// lifetime snapshot. A cumulative observation needs a source-proven baseline.
type CounterMode string

// Counter modes select direct increments or subtraction from a verified
// lifetime baseline; repeated cumulative snapshots are not new consumption.
const (
	CounterIncremental CounterMode = "incremental"
	CounterCumulative  CounterMode = "cumulative"
)

// UsageScope prevents descendant-inclusive evidence being added to child totals.
type UsageScope string

// Usage scopes distinguish additive invocation measurements from inclusive
// summaries retained only as reconciliation evidence.
const (
	UsageExclusive UsageScope = "exclusive"
	UsageInclusive UsageScope = "descendant_inclusive"
)

// UsageObservation is normalized source evidence, not necessarily additive
// usage. EventKey is stable across replay, but must differ for repeated work.
// CounterKey partitions metric/model scopes; CounterEpoch changes only on
// source-proven reset. ActualModel nil means unknown, never configured default.
type UsageObservation struct {
	SourceID        string        `json:"source_id"`
	EventKey        string        `json:"event_key"`
	InvocationID    string        `json:"invocation_id"`
	ActualModel     *string       `json:"actual_model"`
	ModelProvenance string        `json:"model_provenance"`
	Mode            CounterMode   `json:"mode"`
	Scope           UsageScope    `json:"scope"`
	CounterKey      string        `json:"counter_key"`
	CounterEpoch    string        `json:"counter_epoch"`
	Tokens          TokenUsage    `json:"tokens"`
	Coverage        UsageCoverage `json:"coverage"`
}

// Clone gives the receiver independent mutable storage.
func (o UsageObservation) Clone() UsageObservation {
	if o.ActualModel != nil {
		model := *o.ActualModel
		o.ActualModel = &model
	}
	o.Tokens = o.Tokens.Clone()
	o.Coverage = o.Coverage.Clone()
	return o
}

// UsageEvent carries one invocation registration, observation or coverage update.
// Emit rejects ambiguous events instead of silently selecting one payload.
type UsageEvent struct {
	Invocation  *UsageInvocation
	Observation *UsageObservation
	Coverage    *UsageCoverage
}

// Clone copies every nested collection and counter at the callback boundary.
func (e UsageEvent) Clone() UsageEvent {
	if e.Invocation != nil {
		v := *e.Invocation
		v.Coverage = v.Coverage.Clone()
		e.Invocation = &v
	}
	if e.Observation != nil {
		v := e.Observation.Clone()
		e.Observation = &v
	}
	if e.Coverage != nil {
		v := e.Coverage.Clone()
		e.Coverage = &v
	}
	return e
}

// UsageCapture is an optional synchronous persistence seam. The consumer owns
// durability, deduplication and synchronization; adapters call Emit within their
// existing runner lifetime. Sink errors are collection failures, not replacements
// for the subprocess outcome. No goroutine or retry is introduced here.
type UsageCapture struct {
	EvidenceContext func(context.Context) (context.Context, context.CancelFunc)
	Sink            func(context.Context, UsageEvent) error
}

// Emit validates the payload and transfers an independent event to Sink.
func (c UsageCapture) Emit(ctx context.Context, e UsageEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Sink == nil {
		return fmt.Errorf("invalid usage capture: sink required")
	}
	count := 0
	if e.Invocation != nil {
		count++
	}
	if e.Observation != nil {
		count++
	}
	if e.Coverage != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("usage event requires exactly one payload")
	}
	return c.Sink(ctx, e.Clone())
}

// CollectionContext bounds final source reads after execution. A caller may
// supply its existing evidence deadline policy without exposing workflow state.
func (c UsageCapture) CollectionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.EvidenceContext != nil {
		return c.EvidenceContext(ctx)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}

package agent

import (
	"context"
	"fmt"

	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

// TokenUsage preserves the factory wire shape while sharing neutral counters.
type TokenUsage = execution.TokenUsage

// CollectionStatus qualifies evidence independently of process success.
type CollectionStatus = execution.CollectionStatus

// Collection states preserve unknown, partial and complete evidence.
const (
	CollectionUnknown  = execution.CollectionUnknown
	CollectionPartial  = execution.CollectionPartial
	CollectionComplete = execution.CollectionComplete
)

// UsageCoverage retains controlled source diagnostics.
type UsageCoverage = execution.UsageCoverage

// UsageInvocation identifies a root or descendant within a factory attempt.
// ParentID is empty only for a root; unresolved associations are incomplete.
type UsageInvocation struct {
	AttemptID          string        `json:"attempt_id"`
	ID                 string        `json:"id"`
	ParentID           string        `json:"parent_id"`
	SourceID           string        `json:"source_id"`
	SourceInvocationID string        `json:"source_invocation_id"`
	Coverage           UsageCoverage `json:"coverage"`
}

// CounterMode distinguishes incremental and source-proven cumulative counters.
type CounterMode = execution.CounterMode

// Counter modes preserve native source semantics.
const (
	CounterIncremental = execution.CounterIncremental
	CounterCumulative  = execution.CounterCumulative
)

// UsageScope separates exclusive measurements from inclusive summaries.
type UsageScope = execution.UsageScope

// Usage scopes determine whether evidence is additive.
const (
	UsageExclusive = execution.UsageExclusive
	UsageInclusive = execution.UsageInclusive
)

// UsageObservation is normalized source evidence, not necessarily additive
// usage. EventKey is stable across replay, but must differ for repeated work.
// CounterKey partitions metric/model scopes; CounterEpoch changes only on
// source-proven reset. ActualModel nil means unknown, never configured default.
type UsageObservation struct {
	AttemptID       string        `json:"attempt_id"`
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
	AttemptID string
	Sink      func(context.Context, UsageEvent) error
}

// Emit validates attempt ownership and transfers an independent event to Sink.
func (c UsageCapture) Emit(ctx context.Context, e UsageEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.AttemptID == "" || c.Sink == nil {
		return fmt.Errorf("invalid usage capture: attempt and sink required")
	}
	count := 0
	if e.Invocation != nil {
		count++
		if e.Invocation.AttemptID != c.AttemptID {
			return fmt.Errorf("usage invocation belongs to another attempt")
		}
	}
	if e.Observation != nil {
		count++
		if e.Observation.AttemptID != c.AttemptID {
			return fmt.Errorf("usage observation belongs to another attempt")
		}
	}
	if e.Coverage != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("usage event requires exactly one payload")
	}
	return c.Sink(ctx, e.Clone())
}

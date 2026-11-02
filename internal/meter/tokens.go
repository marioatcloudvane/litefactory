package meter

import (
	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

// ErrInvalidUsage marks unusable accounting evidence.
var ErrInvalidUsage = execution.ErrInvalidUsage

// UsageError exposes controlled diagnostic identifiers without source text.
type UsageError = execution.UsageError

// Validate checks known counters and subset relations.
func Validate(u agent.TokenUsage) error { return execution.Validate(u) }

// Normalize applies the caller-selected source cache convention.
func Normalize(u agent.TokenUsage, inputIncludesCacheRead bool) (agent.TokenUsage, error) {
	return execution.Normalize(u, inputIncludesCacheRead)
}

// Delta subtracts a verified baseline without inventing unknown values.
func Delta(current, baseline agent.TokenUsage) (agent.TokenUsage, error) {
	return execution.Delta(current, baseline)
}

// Add combines known counters using checked arithmetic.
func Add(left, right agent.TokenUsage) (agent.TokenUsage, error) { return execution.Add(left, right) }

// Total sums disjoint token dimensions when each is known.
func Total(u agent.TokenUsage) (*int64, error) { return execution.Total(u) }

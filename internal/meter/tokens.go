package meter

import (
	"errors"
	"fmt"
	"math"

	"github.com/cloudvane/litefactory/internal/agent"
)

// ErrInvalidUsage marks unusable accounting evidence. Invalid inputs never
// partially advance a checkpoint or yield an additive measurement.
var ErrInvalidUsage = errors.New("invalid usage")

// UsageError exposes controlled diagnostic identifiers without source text.
type UsageError struct {
	Code      string
	Dimension string
}

func (e *UsageError) Error() string {
	return fmt.Sprintf("%s: %s (%s)", ErrInvalidUsage, e.Code, e.Dimension)
}
func (e *UsageError) Unwrap() error { return ErrInvalidUsage }

func tokenFields(u agent.TokenUsage) [7]*int64 {
	return [7]*int64{u.InputUncached, u.CacheRead, u.CacheWrite, u.CacheWrite5m, u.CacheWrite1h, u.Output, u.Reasoning}
}
func fromFields(p [7]*int64) agent.TokenUsage {
	return agent.TokenUsage{InputUncached: p[0], CacheRead: p[1], CacheWrite: p[2], CacheWrite5m: p[3], CacheWrite1h: p[4], Output: p[5], Reasoning: p[6]}
}

var dimensionNames = [7]string{"input_uncached", "cache_read", "cache_write", "cache_write_5m", "cache_write_1h", "output", "reasoning"}

// Validate checks each known counter and subset relation. Unknown parent counts
// do not turn known subset counts into a total.
func Validate(u agent.TokenUsage) error {
	var faults []error
	for i, p := range tokenFields(u) {
		if p != nil && *p < 0 {
			faults = append(faults, &UsageError{Code: "negative_counter", Dimension: dimensionNames[i]})
		}
	}
	if u.Output != nil && u.Reasoning != nil && *u.Reasoning > *u.Output {
		faults = append(faults, &UsageError{Code: "subset_exceeds_total", Dimension: "reasoning"})
	}
	// Compare using subtraction to avoid overflowing the sum of duration subsets.
	if u.CacheWrite != nil && *u.CacheWrite >= 0 {
		remaining := *u.CacheWrite
		for _, p := range []*int64{u.CacheWrite5m, u.CacheWrite1h} {
			if p != nil && *p >= 0 {
				if *p > remaining {
					faults = append(faults, &UsageError{Code: "subset_exceeds_total", Dimension: "cache_write"})
					break
				}
				remaining -= *p
			}
		}
	}
	return errors.Join(faults...)
}

// Normalize optionally subtracts cache reads from an input counter that includes
// them. Adapters explicitly declare that source convention; it is never guessed.
// Missing cached-input detail makes uncached input unknown, not zero.
func Normalize(u agent.TokenUsage, inputIncludesCacheRead bool) (agent.TokenUsage, error) {
	if err := Validate(u); err != nil {
		return agent.TokenUsage{}, err
	}
	out := u.Clone()
	if inputIncludesCacheRead {
		if u.InputUncached == nil || u.CacheRead == nil {
			out.InputUncached = nil
		} else {
			if *u.CacheRead > *u.InputUncached {
				return agent.TokenUsage{}, &UsageError{Code: "subset_exceeds_total", Dimension: "cache_read"}
			}
			v := *u.InputUncached - *u.CacheRead
			out.InputUncached = &v
		}
	}
	return out, nil
}

// Delta subtracts a verified lifetime baseline. Unknown baseline or current
// dimensions remain unknown; regressing counters require explicit source reset
// evidence and must not be treated as a fresh epoch automatically.
func Delta(current, baseline agent.TokenUsage) (agent.TokenUsage, error) {
	return combine(current, baseline, true)
}

// Add combines complete dimension values with checked arithmetic. An unknown
// operand keeps that dimension unknown. Callers tracking partial known subtotals
// must retain separate coverage instead of coercing unknown values to zero.
func Add(left, right agent.TokenUsage) (agent.TokenUsage, error) { return combine(left, right, false) }

func combine(left, right agent.TokenUsage, subtract bool) (agent.TokenUsage, error) {
	if err := errors.Join(Validate(left), Validate(right)); err != nil {
		return agent.TokenUsage{}, err
	}
	a, b := tokenFields(left), tokenFields(right)
	var out [7]*int64
	var faults []error
	for i := range a {
		if a[i] == nil || b[i] == nil {
			continue
		}
		var v int64
		if subtract {
			if *a[i] < *b[i] {
				faults = append(faults, &UsageError{Code: "regressing_counter", Dimension: dimensionNames[i]})
				continue
			}
			v = *a[i] - *b[i]
		} else {
			if *a[i] > math.MaxInt64-*b[i] {
				faults = append(faults, &UsageError{Code: "counter_overflow", Dimension: dimensionNames[i]})
				continue
			}
			v = *a[i] + *b[i]
		}
		out[i] = &v
	}
	if err := errors.Join(faults...); err != nil {
		return agent.TokenUsage{}, err
	}
	result := fromFields(out)
	if err := Validate(result); err != nil {
		return agent.TokenUsage{}, err
	}
	return result, nil
}

// Total sums only the four disjoint categories. Subsets never increase it. A
// missing category leaves total unknown even when other subtotals are known.
func Total(u agent.TokenUsage) (*int64, error) {
	if err := Validate(u); err != nil {
		return nil, err
	}
	var sum int64
	unknown := false
	for _, p := range []*int64{u.InputUncached, u.CacheRead, u.CacheWrite, u.Output} {
		if p == nil {
			unknown = true
			continue
		}
		if *p > math.MaxInt64-sum {
			return nil, &UsageError{Code: "counter_overflow", Dimension: "total"}
		}
		sum += *p
	}
	if unknown {
		return nil, nil
	}
	return &sum, nil
}

package meter

import (
	"errors"
	"math"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func count(n int64) *int64 { return &n }

func TestNormalizeAndTotal(t *testing.T) {
	t.Parallel()
	u := agent.TokenUsage{InputUncached: count(100), CacheRead: count(40), CacheWrite: count(20), CacheWrite5m: count(10), CacheWrite1h: count(10), Output: count(30), Reasoning: count(5)}
	got, err := Normalize(u, true)
	if err != nil {
		t.Fatal(err)
	}
	if *got.InputUncached != 60 {
		t.Fatalf("uncached=%v", *got.InputUncached)
	}
	total, err := Total(got)
	if err != nil || *total != 150 {
		t.Fatalf("total=%v error=%v", total, err)
	}
	*got.CacheRead = 0
	if *u.CacheRead != 40 {
		t.Fatal("normalize aliases original")
	}
	u.CacheRead = nil
	got, err = Normalize(u, true)
	if err != nil || got.InputUncached != nil {
		t.Fatal("missing cached detail fabricated uncached input")
	}
	total, err = Total(got)
	if err != nil || total != nil {
		t.Fatal("unknown total coerced to zero")
	}
}

func TestCounterFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		left, right agent.TokenUsage
		subtract    bool
		code        string
	}{
		{"negative", agent.TokenUsage{Output: count(-1)}, agent.TokenUsage{}, false, "negative_counter"},
		{"overflow", agent.TokenUsage{Output: count(math.MaxInt64)}, agent.TokenUsage{Output: count(1)}, false, "counter_overflow"},
		{"regression", agent.TokenUsage{Output: count(2)}, agent.TokenUsage{Output: count(3)}, true, "regressing_counter"},
		{"subset", agent.TokenUsage{CacheWrite: count(10), CacheWrite5m: count(7), CacheWrite1h: count(4)}, agent.TokenUsage{}, false, "subset_exceeds_total"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.subtract {
				_, err = Delta(tc.left, tc.right)
			} else {
				_, err = Add(tc.left, tc.right)
			}
			var detail *UsageError
			if !errors.Is(err, ErrInvalidUsage) || !errors.As(err, &detail) || detail.Code != tc.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestDeltaKnownZeroAndMissingBaseline(t *testing.T) {
	t.Parallel()
	got, err := Delta(agent.TokenUsage{InputUncached: count(9), Output: count(4)}, agent.TokenUsage{Output: count(4)})
	if err != nil {
		t.Fatal(err)
	}
	if got.InputUncached != nil || got.Output == nil || *got.Output != 0 {
		t.Fatal("lost unknown/zero distinction")
	}
}

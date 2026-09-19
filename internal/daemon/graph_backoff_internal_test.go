package daemon

import (
	"testing"
	"time"
)

func TestGraphBackoffDeterministicPolicy(t *testing.T) {
	t.Parallel()
	last := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		total   int
		policy  retryPolicy
		delay   time.Duration
		further bool
	}{
		{"initial", 0, retryPolicy{Limit: 3}, 0, true},
		{"healthy loop", 2, retryPolicy{Limit: 3}, 0, true},
		{"healthy loop exhausted", 3, retryPolicy{Limit: 3}, 0, false},
		{"failed limit exhausted", 3, retryPolicy{Limit: 3, ConsecutiveFailures: 1}, 0, false},
		{"past limit", 4, retryPolicy{Limit: 3}, 0, false},
		{"first failure", 1, retryPolicy{ConsecutiveFailures: 1}, time.Second, true},
		{"second failure", 2, retryPolicy{ConsecutiveFailures: 2}, 2 * time.Second, true},
		{"reset after success", 4, retryPolicy{}, 0, true},
		{"first failure after success", 5, retryPolicy{ConsecutiveFailures: 1}, time.Second, true},
		{"custom growth", 3, retryPolicy{ConsecutiveFailures: 3, BaseDelay: 2 * time.Second, GrowthFactor: 3}, 18 * time.Second, true},
		{"unit growth", 1000000000, retryPolicy{ConsecutiveFailures: 1000000000, GrowthFactor: 1}, time.Second, true},
		{"custom cap", 4, retryPolicy{ConsecutiveFailures: 4, Cap: 3 * time.Second}, 3 * time.Second, true},
		{"base above cap", 1, retryPolicy{ConsecutiveFailures: 1, BaseDelay: time.Minute, Cap: time.Second}, time.Second, true},
		{"absolute cap", 1000000000, retryPolicy{ConsecutiveFailures: 1000000000, Cap: 2 * maxGraphBackoff}, maxGraphBackoff, true},
		{"overflow saturates", 2, retryPolicy{ConsecutiveFailures: 2, GrowthFactor: int(^uint(0) >> 1)}, maxGraphBackoff, true},
		{"negative attempts", -1, retryPolicy{}, 0, false},
		{"negative limit", 0, retryPolicy{Limit: -1}, 0, false},
		{"negative failures", 0, retryPolicy{ConsecutiveFailures: -1}, 0, false},
		{"excess failures", 1, retryPolicy{ConsecutiveFailures: 2}, 0, false},
		{"negative base", 1, retryPolicy{ConsecutiveFailures: 1, BaseDelay: -1}, 0, false},
		{"negative growth", 1, retryPolicy{ConsecutiveFailures: 1, GrowthFactor: -1}, 0, false},
		{"negative cap", 1, retryPolicy{ConsecutiveFailures: 1, Cap: -1}, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next, further := nextGraphAttemptAt(tt.total, last, tt.policy)
			want := time.Time{}
			if tt.further {
				want = last.Add(tt.delay)
			}
			if further != tt.further || !next.Equal(want) {
				t.Fatalf("got (%v,%v), want (%v,%v)", next, further, want, tt.further)
			}
			again, againFurther := nextGraphAttemptAt(tt.total, last, tt.policy)
			if again != next || againFurther != further {
				t.Fatal("identical input produced different result")
			}
		})
	}
}

func TestGraphBackoffMonotoneAndCapped(t *testing.T) {
	t.Parallel()
	last := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	previous := time.Duration(0)
	for failures := 1; failures <= 64; failures++ {
		next, further := nextGraphAttemptAt(failures, last, retryPolicy{ConsecutiveFailures: failures})
		delay := next.Sub(last)
		if !further || delay < previous || delay > maxGraphBackoff {
			t.Fatalf("failure %d: delay=%v previous=%v further=%v", failures, delay, previous, further)
		}
		previous = delay
	}
	if previous != maxGraphBackoff {
		t.Fatalf("never reached named cap: %v", previous)
	}
}

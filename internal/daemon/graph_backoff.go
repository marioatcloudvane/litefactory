package daemon

import "time"

// maxGraphBackoff bounds graph retry delays independently of forge backoff.
const maxGraphBackoff = time.Hour

// retryPolicy contains only caller-supplied values. Limit counts every dispatch,
// including healthy loop passes; zero means no explicit limit, as in the store.
// ConsecutiveFailures must be reset to zero after a successful pass. The zero
// delay/growth/cap values select a one-second base, doubling and maxGraphBackoff.
type retryPolicy struct {
	Limit               int
	ConsecutiveFailures int
	BaseDelay           time.Duration
	GrowthFactor        int
	Cap                 time.Duration
}

// nextGraphAttemptAt computes eligibility from the last completed step's time.
// It never reads a clock or changes execution state. A healthy pass can re-enter
// immediately, but still consumes the same per-node dispatch budget as a retry.
// Invalid negative values fail closed; the bool describes permission to attempt,
// not a persisted reservation. This helper deliberately has no production caller.
func nextGraphAttemptAt(totalAttempts int, last time.Time, policy retryPolicy) (time.Time, bool) {
	if totalAttempts < 0 || policy.Limit < 0 || policy.ConsecutiveFailures < 0 ||
		policy.ConsecutiveFailures > totalAttempts || policy.BaseDelay < 0 ||
		policy.GrowthFactor < 0 || policy.Cap < 0 {
		return time.Time{}, false
	}
	if policy.Limit > 0 && totalAttempts >= policy.Limit {
		return time.Time{}, false
	}
	if policy.ConsecutiveFailures == 0 {
		return last, true
	}
	delay := policy.BaseDelay
	if delay == 0 {
		delay = time.Second
	}
	growth := policy.GrowthFactor
	if growth == 0 {
		growth = 2
	}
	capDelay := policy.Cap
	if capDelay == 0 || capDelay > maxGraphBackoff {
		capDelay = maxGraphBackoff
	}
	if delay > capDelay {
		delay = capDelay
	}
	// Saturate before multiplication to prevent overflow. Growth one needs no
	// loop, and every larger factor reaches the cap in at most 63 iterations.
	if growth > 1 {
		factor := time.Duration(growth)
		for failures := 1; failures < policy.ConsecutiveFailures && delay < capDelay; failures++ {
			if delay > capDelay/factor {
				delay = capDelay
				break
			}
			delay *= factor
		}
	}
	return last.Add(delay), true
}

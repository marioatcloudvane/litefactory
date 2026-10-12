package ticket

import "time"

// RateLimitError carries only safe scheduling advice. The adapter's surrounding
// Failure retains dispatch uncertainty; this value grants no retry permission.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (*RateLimitError) Error() string { return "ticket provider quota exhausted" }

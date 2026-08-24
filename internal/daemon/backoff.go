package daemon

import (
	"sync"
	"time"
)

// MaxBackoff caps the wait a run of rate-limit refusals can impose. GitHub's
// primary quota window is an hour, so nothing legitimate needs longer, and a
// daemon that has parked itself for a day is indistinguishable from one that
// has died.
const MaxBackoff = time.Hour

// backoff is the daemon's response to being rate limited (AC-2.12).
//
// It holds *when to resume*, not a duration to sleep for, and that is the whole
// design: a tick that arrives during the hold is skipped rather than delayed,
// so nothing in this package ever blocks on a clock. The daemon resumes on its
// own at the first tick after the hold expires, with the poll cursor and every
// watermark exactly as they were — a rate limit costs time and nothing else.
//
// The delay doubles per consecutive refusal, starting from the delay GitHub
// itself asked for in the response. Doubling matters because the first
// Retry-After is advice about the current window, and a limit that is still
// firing after that window has passed is one litefactory is contributing to.
//
// A backoff is safe for concurrent use.
type backoff struct {
	mu       sync.Mutex
	failures int
	until    time.Time
	delay    time.Duration
}

// hold reports whether ticks are still being skipped, and until when.
func (b *backoff) hold(now time.Time) (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.until.IsZero() || !now.Before(b.until) {
		return time.Time{}, false
	}
	return b.until, true
}

// fail records a rate-limit refusal and returns the hold it imposes: the
// consecutive-failure count, the delay and the instant ticks resume at.
//
// retryAfter is [github.RateLimitError.RetryAfter], which that type guarantees
// is positive; a non-positive value here would mean an immediate retry into a
// limit that is still firing, so it is floored rather than trusted.
func (b *backoff) fail(now time.Time, retryAfter time.Duration) (int, time.Duration, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	b.failures++
	delay := retryAfter
	for i := 1; i < b.failures && delay < MaxBackoff; i++ {
		delay *= 2
	}
	if delay > MaxBackoff {
		delay = MaxBackoff
	}

	b.delay = delay
	b.until = now.Add(delay)
	return b.failures, delay, b.until
}

// clear forgets the run of refusals. It is called after every tick that
// completed, so an isolated rate limit tomorrow starts from GitHub's own
// Retry-After again rather than from an hour of accumulated doubling.
func (b *backoff) clear() (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures == 0 {
		return 0, false
	}
	held := b.delay
	b.failures = 0
	b.delay = 0
	b.until = time.Time{}
	return held, true
}

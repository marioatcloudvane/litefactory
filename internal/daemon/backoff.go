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
// # It gates dispatch and cancels nothing (AC-PAR-4.3)
//
// Everything a backoff owns is in the four fields below: a count, two instants
// and a duration. There is deliberately no context.CancelFunc here and no
// handle on anything running, and none may be added — a rate limit met while
// turns are in flight must not cancel their contexts or kill their claude
// processes. What it does instead is refuse *new* dispatch, at the two points
// [Daemon.Tick], [Daemon.offer], and [Daemon.offerSchedule] check
// [backoff.hold] (AC-PAR-4.4). A run
// that is already past the forge's refusal has work in a worktree that a
// cancellation would throw away, and it will finish or fail on its own.
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

// fail records a rate-limit refusal and returns the hold that stands after it:
// the consecutive-failure count, the delay and the instant ticks resume at.
//
// A refusal that arrives while a hold is already in force does not lengthen it.
// The standing values come back unchanged and the count does not move, because
// the count means *consecutive refusals* and several calls refused inside one
// hold are one refusal that several callers found out about. That distinction
// costs nothing while the daemon does one thing at a time; once a tick's tasks
// run concurrently it is the difference between the delay the forge asked for
// and retryAfter*2^4 for five simultaneous 403s (AC-PAR-4.2).
//
// A refusal *after* a hold has expired still increments, which is what
// "consecutive" always meant: the forge is still refusing after the window it
// named, so litefactory is contributing to the limit rather than waiting it out.
//
// retryAfter is [github.RateLimitError.RetryAfter], which that type guarantees
// is positive; a non-positive value here would mean an immediate retry into a
// limit that is still firing, so it is floored rather than trusted.
func (b *backoff) fail(now time.Time, retryAfter time.Duration) (int, time.Duration, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.until.IsZero() && now.Before(b.until) {
		return b.failures, b.delay, b.until
	}

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

// clear forgets the run of refusals, so that an isolated rate limit tomorrow
// starts from GitHub's own Retry-After again rather than from an hour of
// accumulated doubling.
//
// It is called on evidence of success — the first forge listing of a tick that
// comes back 200, on the Tick goroutine — and not on the tick completing. Those
// were the same thing while a tick did one thing at a time; they stop being the
// same thing once a tick's tasks run concurrently, because a tick can complete
// having had a worker refused. Clearing on the listing means a worker's later
// 429 re-arms the backoff as what it is, a new refusal, instead of being wiped
// out by the tick it happened inside.
//
// The trade runs the other way too: a refusal seen just before the next tick's
// listing succeeds is forgotten, so a limit that is genuinely still firing may
// cost one extra dispatch window before the doubling resumes. That is the worst
// case, and it is cheaper than the alternative — a re-armed hold silently
// cleared by the tick that provoked it.
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

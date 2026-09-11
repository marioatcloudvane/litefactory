package daemon

import (
	"sync"
)

// prepareEscalationAttempts is the consecutive-failure count at which a stuck
// task is called out once. Three matches report recovery's escalation, for the
// same reason: two failures in a row is ordinary bad luck, and a threshold
// that fires on every failure is not an escalation.
const prepareEscalationAttempts = 3

// prepareFailures counts consecutive failed turns per session.
//
// A turn that fails before it consumed its trigger leaves the comment
// unconsumed on purpose: a fetch that failed because the network was down has
// to be retried, not answered with a report that burns the trigger. The cost
// is that nothing ever terminates the retry, so the hundredth identical
// failure reads exactly like the first — the tick logs an error and comes back
// in thirty seconds, forever, and an origin that is permanently gone looks
// like a blip that has not cleared yet.
//
// This is the counter that tells the two apart. It is in memory on purpose:
// the question it answers is "is this task making progress right now", which a
// restart legitimately resets — a daemon that has just come up has not yet
// failed at anything.
type prepareFailures struct {
	mu    sync.Mutex
	items map[int64]prepareFailure
}

// prepareFailure is one session's consecutive-failure state.
type prepareFailure struct {
	// Attempts is how many turns in a row have failed, counting this one.
	Attempts int
	// Escalated records that the threshold line has already been written, so
	// it is written once per stuck spell rather than once per tick.
	Escalated bool
	// Reason is the redacted explanation of the most recent failure. A change
	// of reason is itself worth reporting: a task failing a new way is not the
	// same event as one failing the old way again.
	Reason string
}

// record counts one failure and returns the session's state along with whether
// this failure is the one that should be announced — the first, the one that
// crosses the escalation threshold, or one that fails for a new reason.
func (p *prepareFailures) record(sessionID int64, reason string) (prepareFailure, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items == nil {
		p.items = make(map[int64]prepareFailure)
	}
	previous, existed := p.items[sessionID]
	current := prepareFailure{
		Attempts:  previous.Attempts + 1,
		Escalated: previous.Escalated,
		Reason:    reason,
	}
	escalate := current.Attempts >= prepareEscalationAttempts && !current.Escalated
	if escalate {
		current.Escalated = true
	}
	p.items[sessionID] = current
	return current, !existed || escalate || previous.Reason != reason
}

// clear forgets a session, called where a turn completes without failing. The
// count is of *consecutive* failures: one success means whatever was wrong is
// no longer wrong, and the next bad spell starts from nothing.
func (p *prepareFailures) clear(sessionID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items == nil {
		return
	}
	delete(p.items, sessionID)
}

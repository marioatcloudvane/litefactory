package daemon

import (
	"sync"
	"time"
)

// prepareEscalationAttempts is the consecutive-failure count at which a stuck
// task is called out once. Three matches report recovery's escalation, for the
// same reason: two failures in a row is ordinary bad luck, and a threshold
// that fires on every failure is not an escalation.
const prepareEscalationAttempts = 3

// prepareFailures delays consecutive failed task attempts independently.
// Triggers remain unconsumed for recovery. This is in memory intentionally:
// restart resets transient cadence, while durable report recovery owns delivery.
type prepareFailures struct {
	mu    sync.Mutex
	items map[int64]prepareFailure
}

// prepareFailure is one session's consecutive-failure state.
type prepareFailure struct {
	NextAt time.Time
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
// this failure is the one that should be announced — the first or one that
// fails for a new reason. Escalation is reported separately.
func (p *prepareFailures) record(sessionID int64, reason string, at time.Time, interval time.Duration) (prepareFailure, bool) {
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
	delay := interval
	for i := 1; i < current.Attempts && delay < MaxBackoff; i++ {
		delay *= 2
	}
	if delay > MaxBackoff {
		delay = MaxBackoff
	}
	current.NextAt = at.Add(delay)
	escalate := current.Attempts >= prepareEscalationAttempts && !current.Escalated
	if escalate {
		current.Escalated = true
	}
	p.items[sessionID] = current
	return current, !existed || previous.Reason != reason
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

// held gates task-specific provider calls before materialisation.
func (p *prepareFailures) held(sessionID int64, at time.Time) bool {
	_, held := p.hold(sessionID, at)
	return held
}

func (p *prepareFailures) hold(sessionID int64, at time.Time) (prepareFailure, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	row := p.items[sessionID]
	return row, at.Before(row.NextAt)
}

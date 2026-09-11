package daemon

import "testing"

// A stuck task is announced once, not every tick, and a task that starts
// failing a new way is announced again.
func TestPrepareFailuresEscalateOnceAndOnChange(t *testing.T) {
	var p prepareFailures

	for attempt := 1; attempt < prepareEscalationAttempts; attempt++ {
		state, announce := p.record(7, "origin unreachable")
		if state.Attempts != attempt || state.Escalated {
			t.Fatalf("attempt %d: %+v", attempt, state)
		}
		if announce != (attempt == 1) {
			t.Fatalf("attempt %d announce=%v", attempt, announce)
		}
	}

	state, announce := p.record(7, "origin unreachable")
	if !announce || !state.Escalated || state.Attempts != prepareEscalationAttempts {
		t.Fatalf("threshold did not escalate: %+v announce=%v", state, announce)
	}

	// Still failing the same way: already said.
	if state, announce = p.record(7, "origin unreachable"); announce || state.Attempts != prepareEscalationAttempts+1 {
		t.Fatalf("repeat re-announced: %+v announce=%v", state, announce)
	}

	// Failing a new way is a new fact.
	if _, announce = p.record(7, "worktree is dirty"); !announce {
		t.Fatal("new reason not announced")
	}

	// Sessions do not share a count.
	if state, _ = p.record(8, "origin unreachable"); state.Attempts != 1 {
		t.Fatalf("session 8 inherited a count: %+v", state)
	}

	// Progress resets the spell, including the escalation.
	p.clear(7)
	if state, announce = p.record(7, "origin unreachable"); state.Attempts != 1 || state.Escalated || !announce {
		t.Fatalf("clear did not reset: %+v announce=%v", state, announce)
	}
}

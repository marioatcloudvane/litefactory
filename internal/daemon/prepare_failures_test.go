package daemon

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
	"io"
	"log/slog"
	"testing"
	"time"
)

// A stuck task is announced once, not every tick, and a task that starts
// failing a new way is announced again.
func TestPrepareFailuresEscalateOnceAndOnChange(t *testing.T) {
	var p prepareFailures

	for attempt := 1; attempt < prepareEscalationAttempts; attempt++ {
		state, announce := p.record(7, "origin unreachable", time.Time{}, DefaultInterval)
		if state.Attempts != attempt || state.Escalated {
			t.Fatalf("attempt %d: %+v", attempt, state)
		}
		if announce != (attempt == 1) {
			t.Fatalf("attempt %d announce=%v", attempt, announce)
		}
	}

	state, announce := p.record(7, "origin unreachable", time.Time{}, DefaultInterval)
	if !announce || !state.Escalated || state.Attempts != prepareEscalationAttempts {
		t.Fatalf("threshold did not escalate: %+v announce=%v", state, announce)
	}

	// Still failing the same way: already said.
	if state, announce = p.record(7, "origin unreachable", time.Time{}, DefaultInterval); announce || state.Attempts != prepareEscalationAttempts+1 {
		t.Fatalf("repeat re-announced: %+v announce=%v", state, announce)
	}

	// Failing a new way is a new fact.
	if _, announce = p.record(7, "worktree is dirty", time.Time{}, DefaultInterval); !announce {
		t.Fatal("new reason not announced")
	}

	// Sessions do not share a count.
	if state, _ = p.record(8, "origin unreachable", time.Time{}, DefaultInterval); state.Attempts != 1 {
		t.Fatalf("session 8 inherited a count: %+v", state)
	}

	// Progress resets the spell, including the escalation.
	p.clear(7)
	if state, announce = p.record(7, "origin unreachable", time.Time{}, DefaultInterval); state.Attempts != 1 || state.Escalated || !announce {
		t.Fatalf("clear did not reset: %+v announce=%v", state, announce)
	}
}

func TestPrepareFailureDueTimesAreCappedAndIndependent(t *testing.T) {
	var p prepareFailures
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	delay := DefaultInterval
	for attempt := 1; attempt <= 20; attempt++ {
		state, _ := p.record(7, "temporary", at, DefaultInterval)
		if !state.NextAt.Equal(at.Add(delay)) || state.Attempts != attempt {
			t.Fatalf("attempt %d: %+v, delay %s", attempt, state, delay)
		}
		if !p.held(7, state.NextAt.Add(-time.Nanosecond)) || p.held(7, state.NextAt) || p.held(8, at) {
			t.Fatal("hold boundary or session isolation failed")
		}
		at = state.NextAt
		delay *= 2
		if delay > MaxBackoff {
			delay = MaxBackoff
		}
	}
	p.clear(7)
	if p.held(7, at) {
		t.Fatal("progress retained hold")
	}
	state, _ := p.record(7, "temporary", at, DefaultInterval)
	if state.Attempts != 1 || !state.NextAt.Equal(at.Add(DefaultInterval)) {
		t.Fatalf("reset: %+v", state)
	}
}

func TestPrepareFailureCountsSwallowedAttemptOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{log: logger, interval: DefaultInterval}
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	task := &task{session: store.Session{ID: 7}, log: logger, tickAt: at}
	err := errors.New("temporary failure")
	if e := d.leave(t.Context(), task, logger, "test", err); e != nil {
		t.Fatal(e)
	}
	d.stopped(t.Context(), task, "test", err)
	if state := d.prepFailures.items[7]; state.Attempts != 1 || !state.NextAt.Equal(at.Add(DefaultInterval)) {
		t.Fatalf("duplicate accounting: %+v", state)
	}
}

func TestReportRecoveryPreservesTicketQuotaHold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{log: logger}
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	ctx := context.WithValue(t.Context(), recoveryTickKey{}, at)
	_, err := d.reportAttempt(ctx, store.ReportRecovery{Key: "source:7:8", SessionID: 7, TriggerID: 8}, func() (turn.Posted, error) {
		return turn.Posted{}, &ticket.RateLimitError{RetryAfter: time.Minute}
	})
	if !isRecoveryPending(err) {
		t.Fatalf("lost recovery debt: %v", err)
	}
	until, held := d.backoff.hold(at.Add(30 * time.Second))
	if !held || !until.Equal(at.Add(time.Minute)) {
		t.Fatalf("quota hold=%v until=%v", held, until)
	}
	if d.prepFailures.held(7, at) {
		t.Fatal("report debt also counted as preparation failure")
	}
}

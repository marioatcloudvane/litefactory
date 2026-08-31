package daemon

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBackoffDoublesAndCaps is the "exponential" half of AC-2.12. The first
// wait is the one GitHub asked for; a limit that is still firing after that
// window has passed is one litefactory is contributing to, so each consecutive
// refusal doubles — up to a ceiling, because a daemon parked for a day is
// indistinguishable from one that has died.
//
// Every refusal here is made from beyond the hold the one before it imposed,
// which is what makes them consecutive. Refusals *inside* a hold do not double:
// that is TestBackoffIsIdempotentWithinAHold.
func TestBackoffDoublesAndCaps(t *testing.T) {
	var b backoff
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	const retryAfter = time.Minute

	for i, want := range []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
	} {
		failures, delay, until := b.fail(now, retryAfter)
		if failures != i+1 {
			t.Errorf("refusal %d recorded %d consecutive failures", i+1, failures)
		}
		if delay != want {
			t.Errorf("refusal %d backed off %s, want %s", i+1, delay, want)
		}
		if !until.Equal(now.Add(want)) {
			t.Errorf("refusal %d resumes at %s, want %s", i+1, until, now.Add(want))
		}
		now = until
	}

	// Far enough in to have overflowed the cap several times over.
	for range 20 {
		_, delay, until := b.fail(now, retryAfter)
		if delay > MaxBackoff {
			t.Fatalf("backoff grew to %s, past the %s cap", delay, MaxBackoff)
		}
		now = until
	}
	if _, delay, _ := b.fail(now, retryAfter); delay != MaxBackoff {
		t.Errorf("a long run of refusals settled at %s, want the %s cap", delay, MaxBackoff)
	}
}

// TestBackoffIsIdempotentWithinAHold is AC-PAR-4.2, asserted on the mechanism
// rather than through a tick.
//
// A hold is the answer to one refusal, however many callers run into it. Five
// tasks meeting the same 403 in the same second must impose the delay the forge
// asked for and not retryAfter*2^4 — the count means *consecutive* refusals, and
// those five are one refusal that five callers found out about.
func TestBackoffIsIdempotentWithinAHold(t *testing.T) {
	var b backoff
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	const retryAfter = time.Minute

	failures, delay, until := b.fail(now, retryAfter)
	if failures != 1 || delay != retryAfter {
		t.Fatalf("the first refusal recorded %d failures and %s, want 1 and %s",
			failures, delay, retryAfter)
	}

	// Four more, spread across the hold: at the instant it was armed, a second
	// later, halfway through, and a hair short of its expiry.
	for i, at := range []time.Time{
		now,
		now.Add(time.Second),
		now.Add(30 * time.Second),
		until.Add(-time.Nanosecond),
	} {
		gotFailures, gotDelay, gotUntil := b.fail(at, retryAfter)
		if gotFailures != 1 {
			t.Errorf("refusal %d inside the hold recorded %d consecutive failures, want 1",
				i+2, gotFailures)
		}
		if gotDelay != delay {
			t.Errorf("refusal %d inside the hold moved the delay to %s, want the standing %s",
				i+2, gotDelay, delay)
		}
		if !gotUntil.Equal(until) {
			t.Errorf("refusal %d inside the hold moved the resume instant to %s, want the standing %s",
				i+2, gotUntil, until)
		}
	}

	// A refusal once the hold has expired is a genuinely consecutive one: the
	// forge is still refusing after the window it named itself.
	failures, delay, _ = b.fail(until, retryAfter)
	if failures != 2 {
		t.Errorf("the refusal after the hold expired recorded %d consecutive failures, want 2",
			failures)
	}
	if delay != 2*retryAfter {
		t.Errorf("the refusal after the hold expired backed off %s, want %s", delay, 2*retryAfter)
	}
}

// TestBackoffHoldSkipsUntilItExpires: a tick inside the hold is skipped rather
// than delayed, which is why nothing in this package ever blocks on a clock.
func TestBackoffHoldSkipsUntilItExpires(t *testing.T) {
	var b backoff
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	if _, held := b.hold(now); held {
		t.Error("a fresh backoff is holding ticks back")
	}

	_, delay, until := b.fail(now, 30*time.Second)
	if got, held := b.hold(now.Add(delay / 2)); !held || !got.Equal(until) {
		t.Errorf("mid-hold: held=%v until=%s, want true and %s", held, got, until)
	}
	// The boundary resumes rather than waiting one more tick: the delay is
	// GitHub's own answer to "when may I call again".
	if _, held := b.hold(until); held {
		t.Error("the tick at the resume instant was still skipped")
	}
}

// TestBackoffClearForgetsTheRun: an isolated rate limit tomorrow must start
// from GitHub's Retry-After again, not from an hour of accumulated doubling.
func TestBackoffClearForgetsTheRun(t *testing.T) {
	var b backoff
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	if _, wasHeld := b.clear(); wasHeld {
		t.Error("clearing a backoff that was never set reported a hold")
	}

	_, _, until := b.fail(now, time.Minute)
	// From beyond the first hold, so that this is a second consecutive refusal
	// rather than a second caller finding out about the first.
	b.fail(until, time.Minute)
	held, wasHeld := b.clear()
	if !wasHeld {
		t.Error("clearing an active backoff reported no hold")
	}
	if held != 2*time.Minute {
		t.Errorf("the cleared hold was %s, want 2m", held)
	}
	if _, stillHeld := b.hold(now); stillHeld {
		t.Error("ticks are still being skipped after the backoff was cleared")
	}
	if _, delay, _ := b.fail(now, time.Minute); delay != time.Minute {
		t.Errorf("the next refusal backed off %s, want the fresh 1m", delay)
	}
}

// TestBackoffFloorsANonPositiveDelay: RateLimitError guarantees a positive
// RetryAfter, but retrying instantly into a limit that is still firing is the
// one outcome worth being defensive about.
func TestBackoffFloorsANonPositiveDelay(t *testing.T) {
	var b backoff
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	if _, delay, _ := b.fail(now, 0); delay <= 0 {
		t.Errorf("a zero Retry-After produced a %s backoff", delay)
	}
}

// TestBackoffTouchesNoContext is AC-PAR-4.3, held structurally.
//
// A rate limit met while turns are in flight must gate the dispatch of *new*
// runs and nothing else: it may not cancel a running worker's context or kill
// its claude child. The way that could quietly stop being true is not a
// deliberate decision but an ordinary-looking one — a CancelFunc parked on the
// backoff so that "backing off" could also mean "stop what is running", or a
// context threaded through fail() so that the arming could be abandoned. Both
// of those start by importing context into this file, so that is what this
// refuses.
//
// It is a syntactic check over one file, so what it does not catch is
// everything outside backoff.go: a cancel held on [Daemon] and invoked next to
// a call to fail would be invisible here. That belongs to the dispatch path,
// and the comment at the check in [Daemon.offer] is where it is argued.
func TestBackoffTouchesNoContext(t *testing.T) {
	t.Parallel()

	const file = "backoff.go"

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	for _, spec := range parsed.Imports {
		if spec.Path.Value == `"context"` {
			t.Errorf("%s: %s imports context. The backoff holds an instant and gates dispatch; "+
				"it owns no CancelFunc and touches no worker's context (AC-PAR-4.3)",
				fset.Position(spec.Pos()), file)
		}
	}

	// A rename of the file would otherwise turn this into a test that passes
	// because it looked at nothing.
	if parsed.Name.Name != "daemon" {
		t.Fatalf("%s declares package %s, want daemon", file, parsed.Name.Name)
	}
	if !strings.Contains(string(mustRead(t, file)), "type backoff struct") {
		t.Fatalf("%s no longer declares the backoff type; if it moved, move this check with it", file)
	}
}

// mustRead reads a file in this package or fails the test.
func mustRead(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

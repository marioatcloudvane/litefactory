package daemon

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestLinearWrappedQuotaArmsSharedBackoff(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{log: logger}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	quota := &ticket.Failure{Operation: "reply discovery", Kind: ticket.Unavailable, Cause: &ticket.RateLimitError{RetryAfter: 3 * time.Minute}}
	wrapped := fmt.Errorf("recover project one: %w", quota)
	if !providerRateLimited(wrapped) {
		t.Fatal("wrapped Linear quota was not recognized")
	}
	if err := d.observe(t.Context(), now, wrapped); !errors.Is(err, quota) {
		t.Fatal("quota observation discarded its source failure")
	}
	until, held := d.backoff.hold(now.Add(time.Minute))
	if !held || !until.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("shared backoff=%v held=%v", until, held)
	}
	// A second project using the same publisher joins the standing hold rather
	// than multiplying the delay or starting an independent immediate retry.
	d.rateLimited(t.Context(), logger.With("project", "two"), now.Add(time.Minute), fmt.Errorf("recover project two: %w", quota))
	secondUntil, held := d.backoff.hold(now.Add(2 * time.Minute))
	if !held || secondUntil != until || d.backoff.failures != 1 {
		t.Fatalf("shared hold changed: %v held=%v failures=%d", secondUntil, held, d.backoff.failures)
	}
	if _, held := d.backoff.hold(until); held {
		t.Fatal("Linear quota hold did not release at the retained retry time")
	}

	ordinary := &Daemon{log: logger}
	failure := &ticket.Failure{Operation: "read", Kind: ticket.Unavailable, Cause: errors.New("temporary access failure")}
	ordinary.rateLimited(t.Context(), logger, now, failure)
	if providerRateLimited(failure) {
		t.Fatal("ordinary ticket access failure became quota advice")
	}
	if _, held := ordinary.backoff.hold(now); held {
		t.Fatal("ordinary access failure armed shared quota backoff")
	}
}

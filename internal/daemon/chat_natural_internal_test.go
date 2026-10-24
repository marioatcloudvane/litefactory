package daemon

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/discord"
)

func TestDiscordReadFailuresAreLoggedOncePerState(t *testing.T) {
	var out bytes.Buffer
	reads := &discordReadLog{log: slog.New(slog.NewTextHandler(&out, nil))}
	limited := &discord.IntakeFailure{Status: http.StatusTooManyRequests, RetryAfter: 30 * time.Second}
	for range 10 {
		reads.report(t.Context(), "ordinary message intake", limited)
	}
	reads.report(t.Context(), "conversation access", limited)
	for range 3 {
		reads.report(t.Context(), "ordinary message intake", nil)
	}
	reads.report(t.Context(), "ordinary message intake", &discord.IntakeFailure{Status: http.StatusUnauthorized, Capability: true})
	reads.report(t.Context(), "ordinary message intake", errors.New("natural intake text too large"))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("want one line per state change, got %d:\n%s", len(lines), out.String())
	}
	for i, want := range []string{"kind=\"rate limited\"", "source=\"conversation access\"", "recovered", "kind=unauthorized", "kind=other"} {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("line %d misses %s: %s", i, want, lines[i])
		}
	}
	if !strings.Contains(lines[0], "retry_after=30s") || !strings.Contains(lines[0], "status=429") {
		t.Fatalf("rate limit line misses its wait: %s", lines[0])
	}
}

func TestIntakeStatusNamesTheCause(t *testing.T) {
	if s := intakeStatus(&discord.IntakeFailure{Status: http.StatusTooManyRequests, RetryAfter: 30 * time.Second}, 30*time.Second); !strings.Contains(s, "rate limited") || !strings.Contains(s, "30s") {
		t.Fatalf("rate limit status: %q", s)
	}
	if s := intakeStatus(&discord.IntakeFailure{Status: http.StatusForbidden, Capability: true}, time.Second); !strings.Contains(s, "forbidden") || !strings.Contains(s, "Message Content") {
		t.Fatalf("access status: %q", s)
	}
}

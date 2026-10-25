package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func rateLimitHoldFixture(t *testing.T) (*Daemon, store.ConnectionVersion, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 21, 16, 39, 0, 0, time.UTC)
	d := &Daemon{connections: projectconnection.NewFactory(projectconnection.Options{}), log: slog.New(slog.DiscardHandler), now: func() time.Time { return now }}
	d.quotas.tick(now)
	v := store.ConnectionVersion{ID: 1, RepoID: 1, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/repo", CredentialSource: "env", CredentialEnv: "PRIVATE_REFERENCE"}
	return d, v, now
}

// A credential backoff names the provider rate limit instead of discovery
// starvation, while staying a discovery deferral for every caller (#1022).
func TestRateLimitHoldMessageKeepsDeferralSemantics(t *testing.T) {
	t.Parallel()
	d, v, now := rateLimitHoldFixture(t)
	until := now.Add(9*time.Minute + 31*time.Second)
	d.quotas.forVersion(v).backoff.fail(now, until.Sub(now))
	want := "github rate limit: retry after " + until.Local().Format("15:04")

	_, finish, bindErr := d.bindQuotaRequest(t.Context(), v)
	if finish != nil {
		t.Fatal("held request was bound")
	}
	_, budgetErr := d.quotaBudget(quotaVersionContext(t.Context(), v))
	for name, err := range map[string]error{"bindQuotaRequest": bindErr, "quotaBudget": budgetErr} {
		if err == nil || err.Error() != want {
			t.Fatalf("%s: got %v, want %q", name, err, want)
		}
		if strings.Contains(err.Error(), ErrDiscoveryDeferred.Error()) {
			t.Fatalf("%s still reads as budget deferral: %v", name, err)
		}
		wrapped := &ticket.Failure{Kind: ticket.Unavailable, Operation: "read container", Cause: fmt.Errorf("github: inspect repository: %w", err)}
		for _, e := range []error{err, wrapped} {
			if !errors.Is(e, ErrDiscoveryDeferred) || !errors.Is(e, ticket.ErrDiscoveryDeferred) {
				t.Fatalf("%s: %v is no longer a discovery deferral", name, e)
			}
		}
		if got := wrapped.Error(); !strings.HasSuffix(got, "github: inspect repository: "+want) {
			t.Fatalf("%s: graph-step text %q", name, got)
		}
	}

	// Released backoff: a real budget denial keeps the sentinel text.
	d.quotas.tick(until)
	q := d.quotas.forVersion(v)
	ctx := context.WithValue(t.Context(), quotaReservationKey{}, &discoveryReservation{budget: &q.discovery, fallback: true, finished: true})
	if _, _, err := d.bindQuotaRequest(ctx, v); err == nil || err.Error() != ErrDiscoveryDeferred.Error() {
		t.Fatalf("budget denial: %v", err)
	}
	if b, err := d.quotaBudget(quotaVersionContext(t.Context(), v)); err != nil || b != &q.discovery {
		t.Fatalf("released quota budget: %v", err)
	}
}

func TestIssueReadDeferralReasonNamesRateLimit(t *testing.T) {
	t.Parallel()
	d, v, now := rateLimitHoldFixture(t)
	capture := func() map[string]any {
		var buf bytes.Buffer
		c := &candidate{session: store.Session{ID: 7}, log: slog.New(slog.NewJSONHandler(&buf, nil))}
		d.logDiscoveryDeferral(quotaVersionContext(t.Context(), v), c)
		var line map[string]any
		if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", buf.String(), err)
		}
		return line
	}

	line := capture()
	if _, ok := line["backoff_until"]; ok {
		t.Fatalf("backoff_until without a hold: %v", line)
	}
	switch line["reason"] {
	case pauseReasonStale, pauseReasonHeld, pauseReasonSpent:
	default:
		t.Fatalf("budget reason: %v", line)
	}

	until := now.Add(9 * time.Minute)
	d.quotas.forVersion(v).backoff.fail(now, until.Sub(now))
	line = capture()
	want := "github rate limit: retry after " + until.Local().Format("15:04")
	if line["reason"] != want {
		t.Fatalf("reason %v, want %q", line["reason"], want)
	}
	if _, ok := line["backoff_until"]; !ok {
		t.Fatalf("backoff_until missing: %v", line)
	}
	if line["budget_error"] != want {
		t.Fatalf("budget_error %v", line["budget_error"])
	}
}

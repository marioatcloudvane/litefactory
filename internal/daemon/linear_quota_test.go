package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestLinearWrappedQuotaIsConnectionLocal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	d := &Daemon{connections: projectconnection.NewFactory(projectconnection.Options{}), log: slog.New(slog.DiscardHandler), now: func() time.Time { return now }}
	d.quotas.tick(now)
	version := store.ConnectionVersion{ID: 1, RepoID: 1, Provider: ticket.Linear, Origin: "https://api.linear.app", CredentialSource: "env", CredentialEnv: "PRIVATE_REFERENCE"}
	quota := &ticket.Failure{Operation: "reply discovery", Kind: ticket.Unavailable, Cause: &ticket.RateLimitError{RetryAfter: 3 * time.Minute}}
	wrapped := fmt.Errorf("recover project one: %w", quota)
	if !providerRateLimited(wrapped) {
		t.Fatal("wrapped refusal lost")
	}
	// These requests were already in flight when the first refusal arrived.
	var finishes []func(error)
	for range 8 {
		_, finish, err := d.bindQuotaRequest(t.Context(), version)
		if err != nil {
			t.Fatal(err)
		}
		finishes = append(finishes, finish)
	}
	var wg sync.WaitGroup
	for _, finish := range finishes {
		wg.Go(func() { finish(wrapped) })
	}
	wg.Wait()
	state := d.quotas.forVersion(version)
	until, held := state.backoff.hold(now)
	if !held || until != now.Add(3*time.Minute) || state.backoff.failures != 1 {
		t.Fatalf("hold=%v failures=%d", until, state.backoff.failures)
	}
	shared := version
	shared.ID, shared.RepoID, shared.Destination = 2, 2, "another/project"
	if _, _, err := d.bindQuotaRequest(t.Context(), shared); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("shared reference escaped: %v", err)
	}
	for _, change := range []string{"provider", "origin", "credential"} {
		independent := version
		independent.ID = 3
		switch change {
		case "provider":
			independent.Provider = ticket.GitHub
		case "origin":
			independent.Origin = "https://other.invalid"
		case "credential":
			independent.CredentialEnv = "OTHER_REFERENCE"
		}
		_, finish, err := d.bindQuotaRequest(t.Context(), independent)
		if err != nil {
			t.Fatalf("%s shares hold: %v", change, err)
		}
		finish(nil)
		if _, held := state.backoff.hold(now); !held {
			t.Fatalf("%s success cleared unrelated hold", change)
		}
	}
	// An earlier successful request cannot override a subsequent refusal.
	finishes[0](nil)
	if _, held := state.backoff.hold(now); !held {
		t.Fatal("in-flight success released hold")
	}
	if text := d.scopedPauseSummary(now); !strings.Contains(text, "project 2 linear") || strings.Contains(text, "PRIVATE_REFERENCE") {
		t.Fatalf("unsafe/incomplete status: %s", text)
	}
	now = until
	d.quotas.tick(now)
	_, finish, err := d.bindQuotaRequest(t.Context(), version)
	if err != nil {
		t.Fatal(err)
	}
	finish(wrapped)
	if state.backoff.delay != 6*time.Minute {
		t.Fatalf("doubling lost: %v", state.backoff.delay)
	}
	now = now.Add(6 * time.Minute)
	d.quotas.tick(now)
	_, finish, err = d.bindQuotaRequest(t.Context(), version)
	if err != nil {
		t.Fatal(err)
	}
	finish(nil)
	if state.backoff.failures != 0 || d.scopedPauseSummary(now) != "" {
		t.Fatal("same-scope success did not reset expired hold")
	}
	_, finish, err = d.bindQuotaRequest(t.Context(), version)
	if err != nil {
		t.Fatal(err)
	}
	finish(errors.New("access unavailable"))
	if _, held := state.backoff.hold(now); held {
		t.Fatal("ordinary access error armed hold")
	}
}

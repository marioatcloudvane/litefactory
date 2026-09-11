package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

func budgetFixture(limit, remaining int) (*discoveryBudget, time.Time) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	b := &discoveryBudget{}
	b.tick(now)
	if limit > 0 {
		b.observe(github.RequestObservation{CoreRateValid: true, Limit: limit, Remaining: remaining, Reset: now.Add(time.Hour), StatusCode: 200})
	}
	return b, now
}
func budgetAcquire(t *testing.T, b *discoveryBudget, key string, cost int, probe bool) *discoveryReservation {
	t.Helper()
	r, err := b.acquire(key, cost, probe)
	if err != nil {
		t.Fatalf("acquire %q cost %d: %v", key, cost, err)
	}
	return r
}
func budgetDenied(t *testing.T, b *discoveryBudget, key string, cost int, probe bool) {
	t.Helper()
	r, err := b.acquire(key, cost, probe)
	if !errors.Is(err, ErrDiscoveryDeferred) || r != nil {
		t.Fatalf("acquire %q = %v, %v; want local deferral", key, r, err)
	}
}
func budgetRequest(b *discoveryBudget, r *discoveryReservation, status int, authenticated bool) {
	o := github.RequestObservation{StatusCode: status, Authenticated: authenticated}
	b.observe(o)
	r.observe(o)
}

func TestDiscoveryBudget_QuotaFloor(t *testing.T) {
	for _, tt := range []struct {
		name                      string
		limit, remaining, allowed int
	}{
		{"exact_floor", 5000, 4000, 0}, {"one_above", 5000, 4001, 1}, {"fractional_floor", 7, 7, 1}, {"small_limit", 4, 4, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, _ := budgetFixture(tt.limit, tt.remaining)
			for i := 0; i < tt.allowed; i++ {
				r := budgetAcquire(t, b, "scalar", 1, false)
				budgetRequest(b, r, 200, true)
				r.finish()
			}
			budgetDenied(t, b, "scalar", 1, false)
		})
	}
}

func TestDiscoveryBudget_ConcurrentReservations(t *testing.T) {
	b, _ := budgetFixture(5000, 4100)
	var wg sync.WaitGroup
	reservations := make(chan *discoveryReservation, 32)
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			r, err := b.acquire("history", 100, false)
			if err == nil {
				reservations <- r
			} else if !errors.Is(err, ErrDiscoveryDeferred) {
				t.Errorf("acquire: %v", err)
			}
		})
	}
	wg.Wait()
	close(reservations)
	if len(reservations) != 1 {
		t.Fatalf("admitted %d simultaneous 100-request operations, want 1", len(reservations))
	}
	for r := range reservations {
		r.finish()
	}
	budgetAcquire(t, b, "history", 100, false).finish()
}

func TestDiscoveryBudget_TrafficAndRefunds(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		auth    bool
		primary int
	}{
		{"authenticated_304", 304, true, 0}, {"unauthenticated_304", 304, false, 100}, {"changed_200", 200, true, 100}, {"error", 403, true, 100}, {"transport_failure", 0, true, 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, now := budgetFixture(5000, 5000)
			r := budgetAcquire(t, b, "history", 100, false)
			for range 100 {
				budgetRequest(b, r, tt.status, tt.auth)
			}
			r.finish()
			if b.spent != tt.primary {
				t.Fatalf("primary spending %d, want %d", b.spent, tt.primary)
			}
			// Every network response consumes traffic even if its primary charge is free.
			budgetDenied(t, b, "history", 100, false)
			b.tick(now.Add(39 * time.Second))
			budgetDenied(t, b, "history", 100, false)
			b.tick(now.Add(40 * time.Second))
			budgetAcquire(t, b, "history", 100, false).finish()
		})
	}
}

func TestDiscoveryBudget_UnusedReservations(t *testing.T) {
	for _, requests := range []int{0, 1, 20} {
		t.Run(fmt.Sprint(requests), func(t *testing.T) {
			t.Parallel()
			b, _ := budgetFixture(5000, 5000)
			r := budgetAcquire(t, b, "cancel_or_error", 100, false)
			for range requests {
				budgetRequest(b, r, 500, true)
			}
			r.finish()
			// Early failure/cancellation returns the unused traffic and primary reservation.
			budgetAcquire(t, b, "next_history", 100, false).finish()
		})
	}
}

func TestDiscoveryBudget_BackgroundSpendCap(t *testing.T) {
	b, now := budgetFixture(5000, 5000)
	for i := range 10 {
		b.tick(now.Add(time.Duration(i) * time.Minute))
		b.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(time.Hour), StatusCode: http.StatusNotModified, Authenticated: true})
		r := budgetAcquire(t, b, "history", 100, false)
		for range 100 {
			budgetRequest(b, r, 200, true)
		}
		r.finish()
	}
	b.tick(now.Add(10 * time.Minute))
	// A late same-window header cannot renew either remaining quota or spending allowance.
	b.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(time.Hour), StatusCode: 304, Authenticated: true})
	budgetDenied(t, b, "history", 1, false)
}

func TestDiscoveryBudget_Metadata(t *testing.T) {
	for _, tt := range []struct {
		name        string
		observation github.RequestObservation
	}{
		{"missing", github.RequestObservation{}},
		{"non_core", github.RequestObservation{Limit: 5000, Remaining: 5000}},
		{"zero_limit", github.RequestObservation{CoreRateValid: true, Remaining: 0}},
		{"negative_remaining", github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: -1}},
		{"over_limit", github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5001}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, now := budgetFixture(0, 0)
			o := tt.observation
			o.Reset = now.Add(time.Hour)
			b.observe(o)
			budgetDenied(t, b, "history", 100, false)
			budgetAcquire(t, b, "probe", 1, true).finish()
		})
	}
}

func TestDiscoveryBudget_StaleAndProbeCadence(t *testing.T) {
	for _, startKnown := range []bool{false, true} {
		t.Run(fmt.Sprint(startKnown), func(t *testing.T) {
			t.Parallel()
			b, now := budgetFixture(0, 0)
			if startKnown {
				b, _ = budgetFixture(5000, 5000)
				b.tick(now.Add(2 * time.Minute))
				now = now.Add(2 * time.Minute)
			}
			budgetDenied(t, b, "history", 100, false)
			budgetAcquire(t, b, "probe", 1, true).finish()
			budgetDenied(t, b, "other_repo_probe", 1, true)
			b.tick(now.Add(59 * time.Second))
			budgetDenied(t, b, "probe", 1, true)
			b.tick(now.Add(time.Minute))
			budgetAcquire(t, b, "probe", 1, true).finish()
		})
	}
}

func TestDiscoveryBudget_ResetRequiresFreshEvidence(t *testing.T) {
	b, now := budgetFixture(5000, 4000)
	b.tick(now.Add(2 * time.Minute))
	budgetDenied(t, b, "probe", 1, true)
	// Passing reset does not fabricate a fresh allowance.
	b.tick(now.Add(time.Hour))
	budgetDenied(t, b, "history", 100, false)
	r := budgetAcquire(t, b, "probe", 1, true)
	o := github.RequestObservation{StatusCode: 200, Authenticated: true, CoreRateValid: true, Limit: 5000, Remaining: 4999, Reset: now.Add(2 * time.Hour)}
	b.observe(o)
	r.observe(o)
	r.finish()
	budgetAcquire(t, b, "history", 100, false).finish()
}

func TestDiscoveryBudget_MissingMetadataPreservesReserve(t *testing.T) {
	b, now := budgetFixture(5000, 4001)
	b.observe(github.RequestObservation{StatusCode: 200})
	b.tick(now.Add(2 * time.Minute))
	budgetDenied(t, b, "probe", 1, true)
	budgetDenied(t, b, "history", 100, false)
}

func TestDiscoveryBudget_FairHistoryPriority(t *testing.T) {
	b, now := budgetFixture(5000, 5000)
	r := budgetAcquire(t, b, "initial", 100, false)
	for range 100 {
		budgetRequest(b, r, 304, true)
	}
	r.finish()
	budgetDenied(t, b, "org/first/comments/1", 100, false)
	// A different parent's short work must not spend tokens needed by the waiting history.
	budgetDenied(t, b, "org/second/issue/2", 1, false)
	b.tick(now.Add(40 * time.Second))
	budgetDenied(t, b, "org/second/comments/2", 100, false)
	r = budgetAcquire(t, b, "org/first/comments/1", 100, false)
	// An admitted maximum traversal can consume its entire reservation.
	for range 100 {
		budgetRequest(b, r, 304, true)
	}
	r.finish()
	b.tick(now.Add(90 * time.Second))
	budgetAcquire(t, b, "org/second/issue/2", 1, false).finish()
	budgetAcquire(t, b, "org/second/comments/2", 100, false).finish()
}

func TestDiscoveryBudget_FreshnessBoundary(t *testing.T) {
	b, now := budgetFixture(5000, 5000)
	b.tick(now.Add(2*time.Minute - time.Nanosecond))
	budgetAcquire(t, b, "history", 100, false).finish()
	b.tick(now.Add(2 * time.Minute))
	budgetDenied(t, b, "history", 100, false)
	// An expired/reset historical observation must not make stale state fresh.
	b.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(-time.Second), StatusCode: 304, Authenticated: true})
	budgetDenied(t, b, "history", 100, false)
}

func TestDiscoveryBudget_ForegroundObservationPreservesFloor(t *testing.T) {
	b, _ := budgetFixture(5000, 4100)
	// Foreground requests don't acquire background reservations, but consume real quota.
	for range 100 {
		b.observe(github.RequestObservation{StatusCode: 200, Authenticated: true})
	}
	budgetDenied(t, b, "history", 1, false)
	if b.spent != 0 {
		t.Fatalf("foreground requests charged to background cap: %d", b.spent)
	}
}

func TestDiscoveryBudget_ProbeIsScalarOnly(t *testing.T) {
	b, _ := budgetFixture(0, 0)
	budgetDenied(t, b, "probe_history", 100, true)
	budgetAcquire(t, b, "probe", 1, true).finish()
}

func TestDiscoveryBudget_BurstIsCappedAfterLongRefill(t *testing.T) {
	b, now := budgetFixture(5000, 5000)
	b.tick(now.Add(time.Minute))
	first := budgetAcquire(t, b, "first", 100, false)
	second := budgetAcquire(t, b, "second", 20, false)
	budgetDenied(t, b, "third", 1, false)
	first.finish()
	second.finish()
}

func TestDiscoveryBudget_ConcurrentRefunds(t *testing.T) {
	b, _ := budgetFixture(5000, 4100)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 100 {
		r := budgetAcquire(t, b, fmt.Sprintf("scalar-%d", i), 1, false)
		wg.Go(func() { <-start; budgetRequest(b, r, 304, true); r.finish() })
	}
	close(start)
	wg.Wait()
	if b.spent != 0 || b.reserved != 0 {
		t.Fatalf("after authenticated refunds: spent=%d reserved=%d", b.spent, b.reserved)
	}
	// All 100 refunds restore primary admission but not the network burst.
	r := budgetAcquire(t, b, "twenty", 20, false)
	budgetDenied(t, b, "one_more", 1, false)
	r.finish()
}

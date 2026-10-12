package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
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

// budgetDrainTo spends traffic down to target with authenticated 304 scalars,
// which are refunded their primary charge and so move nothing but the token
// bucket. Tests that care about what a refill restores call this instead of
// assuming the bucket started at any particular size, so discoveryBurst can be
// retuned without rewriting them. The waiter queue must be empty: a queued head
// reserves its own cost and would refuse these scalars.
func budgetDrainTo(t *testing.T, b *discoveryBudget, target int) {
	t.Helper()
	// Bounded, because a refund bug makes the loop condition unreachable: a
	// finish that returned traffic it never used would spin here forever rather
	// than fail, and a test helper that hangs reports nothing at all.
	for i := 0; b.tokens > float64(target); i++ {
		if i > discoveryBurst {
			t.Fatalf("draining to %d did not converge: tokens=%v after %d scalars", target, b.tokens, i)
		}
		r := budgetAcquire(t, b, "drain", 1, false)
		budgetRequest(b, r, http.StatusNotModified, true)
		r.finish()
	}
}

func TestDiscoveryBudget_QuotaFloor(t *testing.T) {
	// remaining is given as an offset from that limit's floor, so the cases
	// keep their meaning when discoveryQuotaDivisor is retuned.
	floorOf := func(limit int) int { return limit - limit/discoveryQuotaDivisor }
	// Headroom above the floor is exactly what may be spent, one scalar each,
	// so the allowance is the offset rather than a separate literal.
	for _, tt := range []struct {
		name              string
		limit, aboveFloor int
	}{
		{"exact_floor", 5000, 0}, {"one_above", 5000, 1}, {"fractional_floor", 7, 7 - floorOf(7)}, {"small_limit", 4, 4 - floorOf(4)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, _ := budgetFixture(tt.limit, floorOf(tt.limit)+tt.aboveFloor)
			for i := 0; i < tt.aboveFloor; i++ {
				r := budgetAcquire(t, b, "scalar", 1, false)
				budgetRequest(b, r, 200, true)
				r.finish()
			}
			budgetDenied(t, b, "scalar", 1, false)
		})
	}
}

func TestDiscoveryBudget_ConcurrentReservations(t *testing.T) {
	// Exactly one listing of headroom above the floor, however the share is set.
	b, _ := budgetFixture(5000, 5000-5000/discoveryQuotaDivisor+discoveryListBound)
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
			// Every network response consumes traffic even if its primary charge is
			// free: the fully used reservation above refunded none of it.
			budgetDrainTo(t, b, 20)
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
	// Exhaust the background share exactly: whole listings, then scalars for
	// the remainder, since the share need not divide into discoveryListBound.
	share := 5000 / discoveryQuotaDivisor
	listings := share / discoveryListBound
	for i := range listings {
		b.tick(now.Add(time.Duration(i) * time.Minute))
		b.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(time.Hour), StatusCode: http.StatusNotModified, Authenticated: true})
		r := budgetAcquire(t, b, "history", 100, false)
		for range 100 {
			budgetRequest(b, r, 200, true)
		}
		r.finish()
	}
	b.tick(now.Add(time.Duration(listings) * time.Minute))
	for range share % discoveryListBound {
		r := budgetAcquire(t, b, "scalar", 1, false)
		budgetRequest(b, r, 200, true)
		r.finish()
	}
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
	b, now := budgetFixture(5000, 5000-5000/discoveryQuotaDivisor)
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
	b, now := budgetFixture(5000, 5000-5000/discoveryQuotaDivisor+1)
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
	budgetDrainTo(t, b, 20)
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
	b, _ := budgetFixture(5000, 5000-5000/discoveryQuotaDivisor+100)
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
	second := budgetAcquire(t, b, "second", discoveryBurst-100, false)
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
	r := budgetAcquire(t, b, "short", 20, false)
	budgetDrainTo(t, b, 0)
	budgetDenied(t, b, "one_more", 1, false)
	r.finish()
}

func TestLinearRequestQuotaFreshnessAndCharging(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	b := &discoveryBudget{}
	b.tick(now)
	r, err := b.acquire("probe", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	valid := linear.RequestObservation{StatusCode: 200, RequestRateValid: true, Limit: 1000, Remaining: 900, Reset: now.Add(time.Hour)}
	for range 3 {
		b.observeLinear(valid)
		r.observeLinear(valid)
	}
	r.finish()
	v := b.view()
	if !v.Fresh || v.Share != 500 || v.Reserve != 500 || v.Spent != 3 || v.InProgress != 0 || v.Burst != discoveryBurst-3 {
		t.Fatalf("probe accounting: %+v", v)
	}
	other := &discoveryBudget{}
	other.tick(now)
	if _, err := other.acquire("ordinary", 1, false); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("borrowed quota: %v", err)
	}
	if ordinary, err := b.acquire("ordinary", 1, false); err != nil {
		t.Fatal(err)
	} else {
		ordinary.finish()
	}
	for _, tt := range []struct {
		name string
		o    linear.RequestObservation
	}{
		{"missing", linear.RequestObservation{}},
		{"expired", linear.RequestObservation{RequestRateValid: true, Limit: 1000, Remaining: 900, Reset: now}},
		{"older window", linear.RequestObservation{RequestRateValid: true, Limit: 1000, Remaining: 900, Reset: now.Add(30 * time.Minute)}},
		{"inconsistent", linear.RequestObservation{RequestRateValid: true, Limit: 1000, Remaining: 1001, Reset: now.Add(time.Hour)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b.tick(now.Add(time.Minute))
			observed := b.observedAt
			remaining := b.remaining
			b.observeLinear(tt.o)
			if b.observedAt != observed || b.remaining != remaining-1 {
				t.Fatalf("invalid evidence refreshed or uncharged: %+v", b.view())
			}
		})
	}
	b.tick(now.Add(2 * time.Minute))
	if _, err := b.acquire("ordinary", 1, false); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatal("stale allowance admitted")
	}
	r, err = b.acquire("probe", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	r.finish()
	if _, err := b.acquire("probe", 1, true); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatal("probe cadence lost")
	}
	valid.Reset = now.Add(2 * time.Hour)
	b.observeLinear(valid)
	if !b.view().Fresh || b.spent != 0 {
		t.Fatal("new valid window did not refresh allowance")
	}
	valid.Remaining = 500
	b.observeLinear(valid)
	b.tick(now.Add(4 * time.Minute))
	if _, err := b.acquire("probe", 1, true); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatal("depleted window allowed probe")
	}
}

func TestLinearFallbackReservationLifetime(t *testing.T) {
	b, now := budgetFixture(0, 0)
	const cost = 5 + 2*discoveryListBound
	r, err := b.acquireFallback("project-a", cost, true)
	if err != nil {
		t.Fatal(err)
	}
	b.tick(now.Add(time.Minute))
	if _, err := b.acquireFallback("project-a", cost, true); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("outstanding worker reused: %v", err)
	}
	if b.view().InProgress != cost {
		t.Fatal("minute rollover released worker capacity")
	}
	var done sync.WaitGroup
	for range 10 {
		done.Go(func() { r.finish() })
	}
	done.Wait()
	if v := b.view(); v.InProgress != 0 || v.Burst != discoveryBurst {
		t.Fatalf("refund not exactly once: %+v", v)
	}
	b.mu.Lock()
	b.tokens = cost - 1
	b.mu.Unlock()
	if _, err := b.acquireFallback("project-a", cost, true); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("underfunded worker: %v", err)
	}
	b.tick(now.Add(2 * time.Minute))
	r, err = b.acquireFallback("project-a", cost, true)
	if err != nil {
		t.Fatal(err)
	}
	r.observeLinear(linear.RequestObservation{})
	r.finish()
	if v := b.view(); v.InProgress != 0 || v.Burst != cost-1+120-1 {
		t.Fatalf("actual request not charged: %+v", v)
	}
}

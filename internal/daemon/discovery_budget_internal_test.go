package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
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
	// keep their meaning when discoveryReserveDivisor is retuned.
	floorOf := func(limit int) int { return limit / discoveryReserveDivisor }
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
			// The discovery share can bind before the reserve on small limits.
			for i := 0; i < min(tt.aboveFloor, tt.limit-floorOf(tt.limit)); i++ {
				r := budgetAcquire(t, b, "scalar", 1, false)
				budgetRequest(b, r, 200, true)
				r.finish()
			}
			budgetDenied(t, b, "scalar", 1, false)
		})
	}
}

func TestDiscoveryBudget_ConcurrentReservations(t *testing.T) {
	// Exactly one listing's admission hold above the floor, however the share
	// is set. A list holds only listAdmission; its pages are charged as used.
	b, _ := budgetFixture(5000, 5000/discoveryReserveDivisor+listAdmission)
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
	share := 5000 - 5000/discoveryReserveDivisor
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
			// Without usable quota data only the token bucket bounds discovery (#793).
			budgetAcquire(t, b, "history", 100, false).finish()
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
			// Stale or missing data no longer starves discovery (#793).
			budgetAcquire(t, b, "history", 100, false).finish()
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
	b, now := budgetFixture(5000, 5000/discoveryReserveDivisor)
	b.tick(now.Add(2 * time.Minute))
	budgetDenied(t, b, "probe", 1, true)
	// Within the window the observed reserve still holds.
	budgetDenied(t, b, "history", 100, false)
	// After reset the old window no longer applies; the token bucket bounds discovery (#793).
	b.tick(now.Add(time.Hour))
	budgetAcquire(t, b, "history", 100, false).finish()
	r := budgetAcquire(t, b, "probe", 1, true)
	o := github.RequestObservation{StatusCode: 200, Authenticated: true, CoreRateValid: true, Limit: 5000, Remaining: 4999, Reset: now.Add(2 * time.Hour)}
	b.observe(o)
	r.observe(o)
	r.finish()
	budgetAcquire(t, b, "history", 100, false).finish()
}

func TestDiscoveryBudget_MissingMetadataPreservesReserve(t *testing.T) {
	b, now := budgetFixture(5000, 5000/discoveryReserveDivisor+1)
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
	// Stale data inside a known window keeps its reserve but does not block (#793).
	b.tick(now.Add(2 * time.Minute))
	budgetAcquire(t, b, "history", 100, false).finish()
	// An expired/reset historical observation must not change the known window.
	b.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(-time.Second), StatusCode: 304, Authenticated: true})
	if b.view().Fresh {
		t.Fatal("expired observation made stale state fresh")
	}
}

func TestDiscoveryBudget_ForegroundObservationPreservesFloor(t *testing.T) {
	b, _ := budgetFixture(5000, 5000/discoveryReserveDivisor+100)
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
	// Lists hold only their admission amount, so fill the burst with
	// operations below list size.
	var held []*discoveryReservation
	for i := range 4 {
		held = append(held, budgetAcquire(t, b, fmt.Sprintf("op-%d", i), discoveryBurst/4-1, false))
	}
	budgetDenied(t, b, "last", 5, false)
	for _, r := range held {
		r.finish()
	}
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
	if !v.Fresh || v.Share != 900 || v.Reserve != 100 || v.Spent != 3 || v.InProgress != 0 || v.Burst != discoveryBurst-3 {
		t.Fatalf("probe accounting: %+v", v)
	}
	other := &discoveryBudget{}
	other.tick(now)
	// Another budget never borrows b's quota data; without data of its own it
	// is bounded only by its token bucket (#793).
	if r, err := other.acquire("ordinary", 1, false); err != nil {
		t.Fatalf("unobserved budget: %v", err)
	} else {
		r.finish()
	}
	if other.view().Fresh {
		t.Fatal("other budget borrowed fresh quota data")
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
	// Stale data inside the known window keeps its reserve but does not block (#793).
	if ordinary, err := b.acquire("ordinary", 1, false); err != nil {
		t.Fatalf("stale allowance within reserve refused: %v", err)
	} else {
		ordinary.finish()
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
	valid.Remaining = 100
	b.observeLinear(valid)
	b.tick(now.Add(3 * time.Minute))
	if _, err := b.acquire("probe", 1, true); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatal("depleted window allowed probe while fresh")
	}
	// Linear refills continuously: stale depleted data permits the probe (#912).
	b.tick(now.Add(4 * time.Minute))
	if r, err := b.acquire("probe", 1, true); err != nil {
		t.Fatalf("stale depleted Linear window refused probe: %v", err)
	} else {
		r.finish()
	}
}

// A spent Linear window must not silence Linear until the recorded reset: once
// its data is stale, one probe a minute may learn that capacity returned (#912).
func TestDiscoveryBudget_LinearSpentWindowProbes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 16, 2, 22, 0, 0, time.UTC)
	b := &discoveryBudget{}
	b.tick(now)
	spent := linear.RequestObservation{StatusCode: 200, RequestRateValid: true, Limit: 5000, Remaining: 5000 / discoveryReserveDivisor, Reset: now.Add(time.Hour)}
	b.observeLinear(spent)
	if reason, _ := b.view().deferral(); reason != pauseReasonSpent {
		t.Fatalf("deferral = %q, want %q", reason, pauseReasonSpent)
	}
	budgetDenied(t, b, "history", 1, false)
	// Fresh depleted data still refuses the probe.
	budgetDenied(t, b, "probe", 1, true)
	// C1: stale data admits one cost-1 probe, at most one per minute.
	b.tick(now.Add(2*time.Minute + time.Second))
	budgetDenied(t, b, "probe", 2, true)
	r := budgetAcquire(t, b, "probe", 1, true)
	b.tick(now.Add(2*time.Minute + 59*time.Second))
	budgetDenied(t, b, "probe", 1, true)
	// C2: the probe's fresh response shows restored capacity and a later reset.
	restored := linear.RequestObservation{StatusCode: 200, RequestRateValid: true, Limit: 5000, Remaining: 4000, Reset: now.Add(time.Hour + 3*time.Minute)}
	b.observeLinear(restored)
	r.observeLinear(restored)
	r.finish()
	budgetAcquire(t, b, "history", 1, false).finish()
	if reason, _ := b.view().deferral(); reason == pauseReasonSpent {
		t.Fatalf("view still reports %q: %+v", reason, b.view())
	}
}

// C3: GitHub keeps its fixed window; a spent stale window permits no probe
// before reset.
func TestDiscoveryBudget_GitHubSpentWindowNoProbe(t *testing.T) {
	t.Parallel()
	b, now := budgetFixture(5000, 5000/discoveryReserveDivisor)
	for _, d := range []time.Duration{2 * time.Minute, 10 * time.Minute, 59 * time.Minute} {
		b.tick(now.Add(d))
		budgetDenied(t, b, "probe", 1, true)
	}
	b.tick(now.Add(time.Hour))
	budgetAcquire(t, b, "probe", 1, true).finish()
}

func TestLinearFallbackReservationLifetime(t *testing.T) {
	b, now := budgetFixture(0, 0)
	const cost = linearPendingBound
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

func TestLinearHistoryStillSerializesLists(t *testing.T) {
	for _, cost := range []int{discoveryListBound, linearHistoryBound} {
		t.Run(fmt.Sprint(cost), func(t *testing.T) {
			d := &Daemon{}
			now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
			d.discovery.tick(now)
			d.discovery.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: now.Add(time.Hour), StatusCode: 200})
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			// One synthetic history owns the gate until the test releases it.
			go func() {
				_, err := discoveryCall(t.Context(), d, "first", cost, func(context.Context) (int, error) { close(entered); <-release; return 0, nil })
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("waiting for list gate")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err := discoveryCall(ctx, d, "second", cost, func(context.Context) (int, error) { t.Error("concurrent list entered occupied gate"); return 0, nil })
			close(release)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("waiting list: %v", err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("waiting for list release")
			}
			if d.discovery.view().InProgress != 0 {
				t.Fatal("list leaked reservation")
			}
		})
	}
}

// C1 (#920): a fresh budget with allowance left is held by requests in
// flight, never reported as stale.
func TestDiscoveryViewDeferralFreshIsNotStale(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 50, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	held := discoveryView{Fresh: true, Limit: 2500, Remaining: 356, Reserve: 250, Spent: 0, Share: 2250, InProgress: 104, Reset: reset, now: now}
	if reason, at := held.deferral(); reason == pauseReasonStale || reason != pauseReasonHeld || !at.IsZero() {
		t.Fatalf("fresh held view = %q %v", reason, at)
	}
	stale := held
	stale.Fresh = false
	if reason, _ := stale.deferral(); reason != pauseReasonStale {
		t.Fatalf("stale view = %q, want %q", reason, pauseReasonStale)
	}
	spent := held
	spent.Remaining = 250
	if reason, at := spent.deferral(); reason != pauseReasonSpent || !at.Equal(reset) {
		t.Fatalf("spent view = %q %v", reason, at)
	}
}

// linearAllowanceWriter replaces the fixture's allowance headers with a fresh
// window near the reported numbers (#920).
type linearAllowanceWriter struct {
	http.ResponseWriter
	remaining int64
	reset     time.Time
}

func (w *linearAllowanceWriter) headers() {
	w.Header().Set("X-RateLimit-Requests-Limit", "2500")
	w.Header().Set("X-RateLimit-Requests-Remaining", fmt.Sprint(w.remaining))
	w.Header().Set("X-RateLimit-Requests-Reset", fmt.Sprint(w.reset.UnixMilli()))
}
func (w *linearAllowanceWriter) WriteHeader(code int) {
	w.headers()
	w.ResponseWriter.WriteHeader(code)
}
func (w *linearAllowanceWriter) Write(body []byte) (int, error) {
	w.headers()
	return w.ResponseWriter.Write(body)
}

// linearHeldBudget puts the owner-wait daemon's Linear budget at the reported
// fresh state (remaining 356 of 2500, reserve 250) and serves Linear through
// hook, which sees the request body and may answer itself.
func linearHeldBudget(t *testing.T, w *linearOwnerWait, hook func(http.ResponseWriter, *http.Request, []byte) bool) (*discoveryBudget, *atomic.Int64) {
	t.Helper()
	reset := time.UnixMilli(time.Now().Add(48 * time.Hour).UnixMilli())
	w.d.quotas.tick(w.now)
	b := &w.d.quotas.forVersion(w.v).discovery
	b.mu.Lock()
	b.linear, b.limit, b.remaining, b.reset, b.spent, b.observedAt = true, 2500, 356, reset, 0, b.now
	b.mu.Unlock()
	var requests atomic.Int64
	original := w.remote.server.Config.Handler
	w.remote.server.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		out := &linearAllowanceWriter{ResponseWriter: rw, remaining: 356 - n, reset: reset}
		if hook != nil && hook(out, r, body) {
			return
		}
		original.ServeHTTP(out, r)
	})
	t.Cleanup(func() { w.remote.server.Config.Handler = original })
	if !b.view().Fresh {
		t.Fatal("fixture budget is not fresh")
	}
	return b, &requests
}

func linearCommentRead(body []byte) bool { return bytes.Contains(body, []byte("comments(")) }

// C2, C4 (#920): a Linear comment read holds only its admission amount while
// in flight, leaves room for another read's 1+history admission, charges the
// pages it used, and releases everything on success, error and cancellation.
func TestDiscoveryCommentsLinearHoldsAdmissionOnly(t *testing.T) {
	for _, outcome := range []string{"success", "error", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			w := newLinearOwnerWait(t)
			var b *discoveryBudget
			entered := make(chan struct{}, 1)
			var midFlight atomic.Bool
			b, requests := linearHeldBudget(t, w, func(rw http.ResponseWriter, r *http.Request, body []byte) bool {
				if !linearCommentRead(body) {
					return false
				}
				midFlight.Store(true)
				if v := b.view(); v.InProgress != listAdmission || !b.affords(1+linearHistoryBound) {
					t.Errorf("mid-flight in_progress=%d remaining=%d affords(1+history)=%t", v.InProgress, v.Remaining, b.affords(1+linearHistoryBound))
				}
				switch outcome {
				case "error":
					http.Error(rw, "unavailable", http.StatusInternalServerError)
					return true
				case "cancelled":
					entered <- struct{}{}
					<-r.Context().Done()
					return true
				}
				return false
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = sessionContext(w.d.discoveryContext(ctx), w.session.ID)
			b.mu.Lock()
			tokens := b.tokens
			b.mu.Unlock()
			before := requests.Load()
			done := make(chan error, 1)
			// Joined below before any assertion on the released budget.
			go func() {
				_, err := w.d.discoveryComments(ctx, w.q.forgeRepo, w.session.IssueNumber, 0)
				done <- err
			}()
			if outcome == "cancelled" {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("comment read never reached Linear")
				}
				cancel()
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("comment read did not return")
			}
			if !midFlight.Load() {
				t.Fatalf("no comment page requested: %v", err)
			}
			switch outcome {
			case "success":
				if err != nil {
					t.Fatalf("comment read: %v", err)
				}
			default:
				if err == nil {
					t.Fatal("failed comment read reported success")
				}
			}
			used := requests.Load() - before
			v := b.view()
			b.mu.Lock()
			gate, after := len(b.lists), b.tokens
			b.mu.Unlock()
			if v.InProgress != 0 || gate != 0 {
				t.Fatalf("after %s: in_progress=%d lists gate=%d", outcome, v.InProgress, gate)
			}
			if outcome == "success" && (int64(v.Spent) != used || int64(tokens-after) != used || used > listAdmission) {
				t.Fatalf("read of %d requests: spent=%d tokens used=%v", used, v.Spent, tokens-after)
			}
		})
	}
}

// C6 (#920): while one worker's Linear comment read is in flight, an owner
// comment on a Linear task is materialised and dispatched in the same tick.
func TestDiscoveryLinearOwnerCommentPassesInFlightRead(t *testing.T) {
	w := newLinearOwnerWait(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var held atomic.Bool
	b, _ := linearHeldBudget(t, w, func(_ http.ResponseWriter, r *http.Request, body []byte) bool {
		if linearCommentRead(body) && held.CompareAndSwap(false, true) {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		return false
	})
	ctx := t.Context()
	done := make(chan error, 1)
	// Worker A's comment read; joined before the test returns.
	go func() {
		_, err := w.d.discoveryComments(sessionContext(w.d.discoveryContext(ctx), w.session.ID), w.q.forgeRepo, w.session.IssueNumber, 0)
		done <- err
	}()
	released := false
	defer func() {
		if !released {
			close(release)
			<-done
		}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker A never reached Linear")
	}
	if v := b.view(); v.InProgress != listAdmission {
		t.Fatalf("worker A holds %d", v.InProgress)
	}
	w.remote.addReply("owner-command", "/spike continue this Linear ticket", "human-uuid")
	linearWorkflowCheck(t, w.d.db.HandoffSubscriptionObservation(ctx, w.sub.ID, w.sub.Revision, store.PendingTicketObservation{RepoID: w.v.RepoID, ConnectionVersionID: w.v.ID, Provider: w.v.Provider, Origin: w.v.Origin, Container: w.v.Destination, ContainerID: w.v.RemoteID, TicketID: "parent-uuid", TicketKey: "parent-uuid", ObservedAt: w.now}))
	w.now = w.now.Add(DefaultInterval)
	res, err := w.d.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.f.logs.String(), "issue read deferred by the discovery budget") {
		t.Fatalf("owner command deferred while a comment read was in flight:\n%s", w.f.logs.String())
	}
	if res.Dispatched != 1 {
		t.Fatalf("owner command not dispatched in the same tick: %+v\n%s", res, w.f.logs.String())
	}
	close(release)
	released = true
	if err := <-done; err != nil {
		t.Fatal("worker A: ", err)
	}
	if err := w.d.Quiesce(ctx); err != nil {
		t.Fatal(err)
	}
	dispatched := false
	for len(w.requests) > 0 {
		dispatched = dispatched || (<-w.requests).SessionID == w.session.ID
	}
	if !dispatched {
		t.Fatalf("owner command did not reach a turn\n%s", w.f.logs.String())
	}
	if v := b.view(); v.InProgress != 0 {
		t.Fatalf("in_progress=%d after all work finished", v.InProgress)
	}
}

// C5 (#920): on a Linear project, managed and graph-Plan parents wait for the
// repository's listing tick unless another source brings them in.
func TestManagedAndPlanParentsLinearListingCadence(t *testing.T) {
	w := newLinearOwnerWait(t)
	ctx := t.Context()
	linearWorkflowCheck(t, w.d.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: w.session.TaskRef, Generation: 1, State: "planning"}))
	_, err := w.d.db.AdmitPlanWorkflowPath(ctx, w.session.ID, "graph")
	linearWorkflowCheck(t, err)
	_, err = w.d.db.ReservePlanStory(ctx, w.session.TaskRef, "US-1")
	linearWorkflowCheck(t, err)
	sources := map[string]func(context.Context, map[int64]*repoQueue, map[int64]*candidate) error{
		"managed_registry": w.d.managedParents, "plan_recovery": w.d.graphPlanParents,
	}
	for name, source := range sources {
		for _, listing := range []bool{false, true} {
			q := &repoQueue{repo: w.q.repo, forgeRepo: w.q.forgeRepo, log: w.q.log, listing: listing}
			linearWorkflowCheck(t, source(ctx, map[int64]*repoQueue{q.repo.ID: q}, map[int64]*candidate{}))
			if want := map[bool]int{false: 0, true: 1}[listing]; len(q.candidates) != want {
				t.Fatalf("%s listing=%t offered %d candidates, want %d", name, listing, len(q.candidates), want)
			}
		}
		// Another source already brought the parent in this tick.
		q := &repoQueue{repo: w.q.repo, forgeRepo: w.q.forgeRepo, log: w.q.log}
		ref, err := naming.ParseRef(w.session.TaskRef)
		linearWorkflowCheck(t, err)
		c := q.candidate(w.session, ref)
		seen := map[int64]*candidate{w.session.ID: c}
		linearWorkflowCheck(t, source(ctx, map[int64]*repoQueue{q.repo.ID: q}, seen))
		if !slices.Contains(c.sources, name) {
			t.Fatalf("%s did not join the existing candidate: %v", name, c.sources)
		}
	}
	for i := range 10 {
		if n, dispatched := w.heartbeat(t); n != 0 || len(dispatched) != 0 {
			t.Fatalf("heartbeat %d: %d Linear requests, dispatched %v\n%s", i, n, dispatched, w.f.logs.String())
		}
	}
	linearWorkflowCheck(t, w.d.db.SetSessionNeedsPoll(ctx, w.session.ID))
	if n, _ := w.heartbeat(t); n == 0 {
		t.Fatal("a parent owed a poll was not read on a heartbeat without listing")
	}
}

// C5 (#920): without a Linear connection, managed and graph-Plan parents are
// still offered on every tick.
func TestManagedAndPlanParentsGitHubEveryTick(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	task, _ := f.task(t, "a")
	ctx := t.Context()
	_, err := f.d.db.AdmitPlanWorkflowPath(ctx, task.session.ID, "graph")
	linearWorkflowCheck(t, err)
	_, err = f.d.db.ReservePlanStory(ctx, task.session.TaskRef, "US-1")
	linearWorkflowCheck(t, err)
	for _, source := range []func(context.Context, map[int64]*repoQueue, map[int64]*candidate) error{f.d.managedParents, f.d.graphPlanParents} {
		q := &repoQueue{repo: task.repo, forgeRepo: task.forgeRepo, log: f.d.log}
		linearWorkflowCheck(t, source(ctx, map[int64]*repoQueue{q.repo.ID: q}, map[int64]*candidate{}))
		if len(q.candidates) != 1 {
			t.Fatalf("GitHub parent offered %d candidates on a heartbeat", len(q.candidates))
		}
	}
}

package daemon

import (
	"testing"
	"time"
)

// The live Linear starvation of 2026-10-14: 354 of 2500 left, reserve 250. A
// backlog drain needs its issue read plus a 104-request history reservation,
// so it must not start (and must not spend the issue read) until the window
// has room for both.
func TestDiscoveryBudgetAffordsWholeDrainOnly(t *testing.T) {
	now := time.Date(2026, 10, 14, 11, 0, 0, 0, time.UTC)
	b := &discoveryBudget{limit: 2500, remaining: 354, reset: now.Add(30 * time.Minute), now: now, tokens: 400}
	if !b.affords(1) {
		t.Fatal("a single read above the reserve must fit")
	}
	if b.affords(1 + linearHistoryBound) {
		t.Fatal("issue read plus history reservation would cross the reserve; the drain must be deferred")
	}
	b.remaining = 1 + linearHistoryBound + b.limit/discoveryReserveDivisor
	if !b.affords(1 + linearHistoryBound) {
		t.Fatal("a window with room for the whole drain must afford it")
	}
	b.tokens = 10
	if b.affords(1 + linearHistoryBound) {
		t.Fatal("the token bucket still bounds the drain")
	}
	stale := &discoveryBudget{now: now, tokens: 400}
	if !stale.affords(1 + linearHistoryBound) {
		t.Fatal("without a known window only the token bucket applies")
	}
}

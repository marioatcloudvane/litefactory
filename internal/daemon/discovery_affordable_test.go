package daemon

import (
	"testing"
	"time"
)

// The live Linear starvation of 2026-10-14: 354 of 2500 left, reserve 250. A
// backlog drain starts when its first history page fits above the reserve,
// and must not spend its issue read when even that does not fit.
func TestDiscoveryBudgetAffordsWholeDrainOnly(t *testing.T) {
	now := time.Date(2026, 10, 14, 11, 0, 0, 0, time.UTC)
	b := &discoveryBudget{limit: 2500, remaining: 354, reset: now.Add(30 * time.Minute), now: now, tokens: 400}
	if !b.affords(1) {
		t.Fatal("a single read above the reserve must fit")
	}
	if !b.affords(1 + linearHistoryBound) {
		t.Fatal("a history read needs only its first page above the reserve to start")
	}
	b.remaining = b.limit/discoveryReserveDivisor + listAdmission - 1
	if b.affords(1 + linearHistoryBound) {
		t.Fatal("without room for the first page above the reserve the drain must be deferred")
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

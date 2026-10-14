package daemon

import (
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestWebhookRecoveryEligibility(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	original := github.HookDelivery{ID: 1, GUID: "g", OriginalKnown: true, Event: "issue_comment", StatusCode: 502, DeliveredAt: cutoff.Add(time.Second)}
	for _, tc := range []struct {
		name   string
		mutate func(*hookRecoveryState, []github.HookDelivery)
		retry  bool
	}{
		{"uninterrupted", func(*hookRecoveryState, []github.HookDelivery) {}, true},
		{"prestart", func(s *hookRecoveryState, _ []github.HookDelivery) { s.cutoff = cutoff.Add(time.Minute) }, false},
		{"equal timestamp", func(s *hookRecoveryState, _ []github.HookDelivery) { s.cutoff = original.DeliveredAt }, false},
		{"unknown original flag", func(_ *hookRecoveryState, g []github.HookDelivery) { g[0].OriginalKnown = false }, false},
		{"missing original", func(_ *hookRecoveryState, g []github.HookDelivery) {
			g[0].OriginalKnown = false
			g[0].Redelivery = true
		}, false},
		{"success", func(_ *hookRecoveryState, g []github.HookDelivery) { g[0].StatusCode = 202 }, false},
		{"unsupported", func(_ *hookRecoveryState, g []github.HookDelivery) { g[0].Event = "push" }, false},
		{"evicted accounting", func(s *hookRecoveryState, _ []github.HookDelivery) { s.pollingOnly = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newHookRecovery(store.TicketSubscription{}, 1)
			s.cutoff = cutoff
			g := []github.HookDelivery{original}
			tc.mutate(s, g)
			_, retry, reason := s.decision(g, cutoff.Add(2*time.Minute))
			if retry != tc.retry {
				t.Fatalf("retry=%t reason=%s", retry, reason)
			}
		})
	}
}

func TestWebhookRecoveryReconcilesAndBoundsAttempts(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	s := newHookRecovery(store.TicketSubscription{}, 1)
	s.cutoff = cutoff
	g := []github.HookDelivery{{ID: 1, GUID: "g", OriginalKnown: true, Event: "issue_comment", StatusCode: 502, DeliveredAt: cutoff.Add(time.Second)}}
	now := cutoff.Add(time.Minute)
	for attempt := 1; attempt <= 3; attempt++ {
		_, retry, reason := s.decision(g, now)
		if !retry {
			t.Fatalf("attempt %d: %s", attempt, reason)
		}
		intent := s.intents["g"]
		intent.count++
		intent.pending = true
		intent.pendingID = int64(attempt + 1)
		intent.last = now
		// Both 202 and a lost reply retain this exact pending intent until visible.
		for range 3 {
			if _, again, _ := s.decision(g, now.Add(5*time.Minute)); again {
				t.Fatal("blind POST before reconciliation")
			}
		}
		g = append(g, github.HookDelivery{ID: int64(attempt + 1), GUID: "g", Redelivery: true, Event: "issue_comment", StatusCode: 502, DeliveredAt: now.Add(time.Second)})
		if _, again, _ := s.decision(g, now.Add(30*time.Second)); again {
			t.Fatal("retry before one minute")
		}
		now = now.Add(time.Minute)
	}
	if _, retry, reason := s.decision(g, now); retry || reason != "redelivery exhausted" {
		t.Fatalf("retry=%t reason=%s", retry, reason)
	}
	// Identical visible histories after restart cannot distinguish no POST from
	// an accepted/timed-out POST whose remote attempt has not appeared yet.
	for _, history := range [][]github.HookDelivery{g[:1], g} {
		restarted := newHookRecovery(store.TicketSubscription{}, 1)
		restarted.cutoff = now
		for range 3 {
			if _, retry, _ := restarted.decision(history, now.Add(time.Hour)); retry {
				t.Fatal("restart restored allowance")
			}
		}
	}
}

func TestWebhookRecoveryCrossPageSuccessAndCapacity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	s := newHookRecovery(store.TicketSubscription{}, 1)
	failed := github.HookDelivery{ID: 1, GUID: "g", OriginalKnown: true, Event: "issue_comment", StatusCode: 502, DeliveredAt: now.Add(time.Minute)}
	if s.addPage(github.HookDeliveryPage{ServerTime: now, Next: "next", Deliveries: []github.HookDelivery{failed}}) {
		t.Fatal("incomplete history accepted")
	}
	success := failed
	success.ID = 2
	success.StatusCode = 200
	success.Redelivery = true
	success.OriginalKnown = false
	if !s.addPage(github.HookDeliveryPage{ServerTime: now, Deliveries: []github.HookDelivery{success}}) {
		t.Fatal("complete history rejected")
	}
	if len(s.groups) != 1 {
		t.Fatal("GUID not grouped across pages")
	}
	if _, retry, _ := s.decision(s.groups[0], now.Add(time.Hour)); retry {
		t.Fatal("retried successful GUID")
	}
	s.resetPage()
	oversized := make([]github.HookDelivery, hookRecoveryCapacity+1)
	for i := range oversized {
		oversized[i] = failed
		oversized[i].ID = int64(i + 1)
	}
	if s.addPage(github.HookDeliveryPage{Deliveries: oversized}) || !s.pollingOnly || len(s.history) > hookRecoveryCapacity {
		t.Fatal("history overflow did not revoke eligibility")
	}
	if _, retry, _ := s.decision([]github.HookDelivery{failed}, now.Add(time.Hour)); retry {
		t.Fatal("overflow restored allowance")
	}
}

func TestWebhookRecoveryAmbiguousReceiptAndForgottenIntents(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	original := github.HookDelivery{ID: 1, GUID: "g", OriginalKnown: true, Event: "issue_comment", StatusCode: 502, DeliveredAt: cutoff.Add(time.Second)}
	observed := original
	observed.ID = 2
	observed.Redelivery = true
	observed.OriginalKnown = false
	state := newHookRecovery(store.TicketSubscription{}, 1)
	state.cutoff = cutoff
	state.intents["g"] = &hookIntent{count: 1, pending: true, last: cutoff}
	// A new remote attempt could be somebody else's manual request. With no
	// explicit attempt identity in the acknowledgment it cannot settle ours.
	if _, retry, reason := state.decision([]github.HookDelivery{original, observed}, cutoff.Add(time.Hour)); retry || reason != "unreconciled request" {
		t.Fatalf("ambiguous request: retry=%t reason=%s", retry, reason)
	}
	state = newHookRecovery(store.TicketSubscription{}, 1)
	state.cutoff = cutoff
	for i := range hookRecoveryCapacity {
		state.intents[strconv.Itoa(i)] = &hookIntent{pending: true, count: 1}
	}
	if _, retry, _ := state.decision([]github.HookDelivery{original}, cutoff.Add(time.Hour)); retry || !state.pollingOnly || len(state.intents) != 0 {
		t.Fatal("capacity discarded accounting without revoking eligibility")
	}
	if _, retry, _ := state.decision([]github.HookDelivery{original}, cutoff.Add(2*time.Hour)); retry {
		t.Fatal("forgotten GUID received new allowance")
	}
}

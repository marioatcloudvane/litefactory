package daemon

import (
	"testing"
	"time"
)

func TestHintSetCollapsesDuplicatesAndBoundsItself(t *testing.T) {
	s := newHintSet(3)
	for range 10 {
		s.add(hintKey{1, 5})
	}
	if got := s.pending(); len(got) != 1 {
		t.Fatalf("pending = %v, want one", got)
	}
	if s.takeOverflow() {
		t.Error("duplicates raised overflow")
	}

	s.add(hintKey{1, 6})
	s.add(hintKey{1, 7})
	s.add(hintKey{1, 8}) // beyond the bound: dropped
	if got := s.pending(); len(got) != 3 || got[2] != (hintKey{1, 7}) {
		t.Fatalf("pending = %v, want the first three in order", got)
	}
	if !s.takeOverflow() {
		t.Error("overflow not reported")
	}
	if s.takeOverflow() {
		t.Error("overflow reported twice")
	}

	s.resolve(hintKey{1, 6})
	s.resolve(hintKey{9, 9}) // unknown: no-op
	if got := s.pending(); len(got) != 2 {
		t.Errorf("pending = %v after resolve", got)
	}
}

func TestHintSetDropsAHintThatKeepsFailingAndAsksForAListing(t *testing.T) {
	s := newHintSet(10)
	k := hintKey{1, 5}
	s.add(k)
	for i := 1; i < maxHintAttempts; i++ {
		if s.failed(k) {
			t.Fatalf("dropped after %d failures", i)
		}
	}
	if !s.failed(k) {
		t.Fatal("not dropped after the last attempt")
	}
	if len(s.pending()) != 0 || !s.takeOverflow() {
		t.Error("a dropped hint must leave the set and request a full listing")
	}
	if s.failed(k) {
		t.Error("failing an unknown hint reported a drop")
	}
}

func TestWebhookRuntimeWakeIsNonBlocking(t *testing.T) {
	w := newWebhookRuntime(WebhookOptions{}, nil)
	for range 5 {
		w.Wake(1, 5)
	}
	if len(w.wake) != 1 {
		t.Errorf("wake signals = %d, want 1", len(w.wake))
	}
}

func TestWebhookLookupIsCaseInsensitiveByKeyAndEmptyWithoutSecret(t *testing.T) {
	w := newWebhookRuntime(WebhookOptions{}, nil)
	w.repos[webhookKey("Acme", "API")] = &webhookRepo{id: 3, secret: []byte("x")}
	id, secret, found := w.Lookup("acme/api")
	if !found || id != 3 || string(secret) != "x" {
		t.Errorf("Lookup = %d, %q, %v", id, secret, found)
	}
	if _, _, found := w.Lookup("acme/other"); found {
		t.Error("an unknown repository was found")
	}
}

func TestDiscoveryScheduleForceDueUntilAListingFinishes(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s := newDiscoverySchedule(time.Hour)
	s.ListingFinished(1, t0)
	if s.Due(1, t0.Add(time.Minute)) {
		t.Fatal("due inside the interval")
	}
	s.ForceDue()
	if !s.Due(1, t0.Add(time.Minute)) || !s.Due(1, t0.Add(2*time.Minute)) {
		t.Fatal("a forced schedule stopped being due before a listing finished")
	}
	s.ListingFinished(1, t0.Add(3*time.Minute))
	if s.Due(1, t0.Add(4*time.Minute)) {
		t.Error("still due after the forced listing finished")
	}
}

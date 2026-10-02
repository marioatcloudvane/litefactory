package daemon

import (
	"testing"
	"time"
)

func TestWebhookModeWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		listening, delivered, hook bool
		want                       string
	}{
		{false, false, false, ModePollingOnly},
		{false, false, true, ModePollingOnly},
		{false, true, true, ModePollingOnly},
		{true, true, false, ModeWebhook},
		{true, true, true, ModeWebhook},
		{true, false, true, ModeWebhookIdle},
		{true, false, false, ModePollingOnly},
	} {
		if got := webhookMode(tc.listening, tc.delivered, tc.hook); got != tc.want {
			t.Errorf("webhookMode(listening=%v delivered=%v hook=%v) = %q, want %q", tc.listening, tc.delivered, tc.hook, got, tc.want)
		}
	}
}

func TestAnnouncedSetIsFilledPerRepoAndClearedWhenListingFinishes(t *testing.T) {
	t.Parallel()
	s := newDiscoverySchedule(time.Hour)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	if set, complete := s.Announced(1); len(set) != 0 || !complete {
		t.Fatalf("unknown repo = %v complete=%v", set, complete)
	}
	s.Announce(1, 7)
	s.Announce(1, 7)
	s.Announce(2, 9)
	set, complete := s.Announced(1)
	if len(set) != 1 || !complete {
		t.Fatalf("repo 1 = %v complete=%v, want just 7", set, complete)
	}
	if _, ok := set[7]; !ok {
		t.Error("7 missing")
	}
	// The returned set is a copy.
	delete(set, 7)
	if again, _ := s.Announced(1); len(again) != 1 {
		t.Error("mutating the returned set changed the schedule")
	}

	s.ListingFinished(1, at)
	if set, _ := s.Announced(1); len(set) != 0 {
		t.Errorf("repo 1 still announced %v after its listing finished", set)
	}
	if set, _ := s.Announced(2); len(set) != 1 {
		t.Errorf("repo 2 lost its set when repo 1 finished: %v", set)
	}
	// A failed listing keeps the set: the listing did not finish.
	s.ListingFailed(2)
	if set, _ := s.Announced(2); len(set) != 1 {
		t.Errorf("a failed listing cleared the set: %v", set)
	}
}

func TestAnnouncedSetIsBounded(t *testing.T) {
	t.Parallel()
	s := newDiscoverySchedule(time.Hour)
	for i := 1; i <= maxAnnounced+5; i++ {
		s.Announce(1, i)
	}
	set, complete := s.Announced(1)
	if len(set) != maxAnnounced || complete {
		t.Fatalf("len=%d complete=%v, want %d and incomplete", len(set), complete, maxAnnounced)
	}
	s.ListingFinished(1, time.Now())
	if set, complete := s.Announced(1); len(set) != 0 || !complete {
		t.Errorf("after the listing: len=%d complete=%v", len(set), complete)
	}
}

package daemon

import (
	"testing"
	"time"
)

var scheduleT0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestDiscoveryScheduleDueAtStartAndOneIntervalAfterFinish(t *testing.T) {
	s := newDiscoverySchedule(60 * time.Minute)

	if !s.Due(1, scheduleT0) {
		t.Fatal("a fresh schedule is not due at start")
	}
	if _, known := s.Snapshot(1); known {
		t.Fatal("a fresh schedule knows a repository")
	}
	s.ListingFailed(1)
	if !s.Due(1, scheduleT0.Add(30*time.Second)) {
		t.Fatal("an unfinished listing stopped being due")
	}

	s.ListingFinished(1, scheduleT0.Add(5*time.Second))
	for _, tc := range []struct {
		name string
		at   time.Duration
		want bool
	}{
		{"next heartbeat", 35 * time.Second, false},
		{"just before the interval", 5*time.Second + 60*time.Minute - time.Nanosecond, false},
		{"exactly one interval after the finish", 5*time.Second + 60*time.Minute, true},
		{"later", 2 * time.Hour, true},
	} {
		if got := s.Due(1, scheduleT0.Add(tc.at)); got != tc.want {
			t.Errorf("%s: Due = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDiscoveryScheduleZeroIntervalIsAlwaysDue(t *testing.T) {
	s := newDiscoverySchedule(0)
	s.ListingFinished(1, scheduleT0)
	if !s.Due(1, scheduleT0) || !s.Due(1, scheduleT0.Add(time.Second)) {
		t.Fatal("a zero interval gated a listing")
	}
}

func TestDiscoveryScheduleForceDueMakesEveryRepoDueUntilItsListingFinishes(t *testing.T) {
	s := newDiscoverySchedule(time.Hour)
	s.ListingFinished(1, scheduleT0)
	s.ListingFinished(2, scheduleT0)
	at := scheduleT0.Add(time.Minute)
	if s.Due(1, at) || s.Due(2, at) {
		t.Fatal("due inside the interval")
	}
	s.ForceDue()
	if !s.Due(1, at) || !s.Due(2, at) || !s.Due(3, at) {
		t.Fatal("a forced schedule is not due for every repository")
	}
	s.ListingFinished(1, scheduleT0.Add(3*time.Minute))
	if s.Due(1, scheduleT0.Add(4*time.Minute)) {
		t.Error("still due after its forced listing finished")
	}
	if !s.Due(2, scheduleT0.Add(4*time.Minute)) {
		t.Error("another repository's listing cleared the force")
	}
}

func TestDiscoveryScheduleWebhooksDoNotPostponeTheCountdown(t *testing.T) {
	s := newDiscoverySchedule(time.Hour)
	for _, finished := range []time.Time{scheduleT0, scheduleT0.Add(2 * time.Hour)} {
		s.ListingFinished(1, finished)
		deadline := finished.Add(time.Hour)
		for _, elapsed := range []time.Duration{10 * time.Minute, 50 * time.Minute, time.Hour - time.Nanosecond, time.Hour, 70 * time.Minute} {
			now := finished.Add(elapsed)
			s.WebhookAccepted(1, now)
			if got, want := s.Due(1, now), !now.Before(deadline); got != want {
				t.Errorf("Due at %s = %v, want %v", now, got, want)
			}
			snap, _ := s.Snapshot(1)
			if !snap.NextDue.Equal(deadline) || !snap.LastWebhook.Equal(now) || !snap.LastListing.Equal(finished) {
				t.Errorf("snapshot at %s = %+v, want deadline %s, webhook %s and listing %s", now, snap, deadline, now, finished)
			}
		}
	}
}

func TestDiscoveryScheduleWebhookNeverMovesAnotherRepo(t *testing.T) {
	s := newDiscoverySchedule(60 * time.Minute)
	s.ListingFinished(1, scheduleT0)
	s.ListingFinished(2, scheduleT0)
	s.WebhookAccepted(1, scheduleT0.Add(50*time.Minute))
	if !s.Due(2, scheduleT0.Add(60*time.Minute)) {
		t.Error("repo 2 was postponed by repo 1's webhook")
	}
	if !s.Due(1, scheduleT0.Add(60*time.Minute)) {
		t.Error("repo 1 was postponed by its own webhook")
	}
}

func TestDiscoveryScheduleWebhookBeforeFirstListingStillListsAtStart(t *testing.T) {
	s := newDiscoverySchedule(time.Hour)
	s.WebhookAccepted(1, scheduleT0)
	if !s.Due(1, scheduleT0.Add(time.Second)) {
		t.Error("a webhook postponed the start-up listing")
	}
}

func TestDiscoveryScheduleOwedSurvivesWebhooksUntilListingFinishes(t *testing.T) {
	s := newDiscoverySchedule(60 * time.Minute)
	s.ListingFinished(1, scheduleT0)
	s.ListingFailed(1)
	s.WebhookAccepted(1, scheduleT0.Add(10*time.Minute))
	if !s.Due(1, scheduleT0.Add(11*time.Minute)) {
		t.Fatal("an owed repository stopped being due after a webhook")
	}
	if snap, _ := s.Snapshot(1); !snap.Owed {
		t.Error("snapshot does not report owed")
	}
	s.ListingFinished(1, scheduleT0.Add(12*time.Minute))
	if s.Due(1, scheduleT0.Add(13*time.Minute)) {
		t.Error("still due after the owed listing finished")
	}
	if snap, _ := s.Snapshot(1); snap.Owed || !snap.LastListing.Equal(scheduleT0.Add(12*time.Minute)) ||
		!snap.LastWebhook.Equal(scheduleT0.Add(10*time.Minute)) {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestDiscoveryScheduleForgetDropsTheEntry(t *testing.T) {
	s := newDiscoverySchedule(time.Hour)
	s.ListingFinished(1, scheduleT0)
	s.Forget(1)
	if _, known := s.Snapshot(1); known || len(s.Known()) != 0 {
		t.Error("entry survived Forget")
	}
	if !s.Due(1, scheduleT0.Add(time.Second)) {
		t.Error("a forgotten repository is not due like a new one")
	}
}

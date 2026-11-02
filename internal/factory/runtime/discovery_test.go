package runtime

import (
	"sync"
	"testing"
	"time"
)

func TestDiscoveryScheduleConcurrentRepositoriesKeepIndependentDeadlines(t *testing.T) {
	t.Parallel()
	schedule := NewDiscoverySchedule(time.Hour)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var workers sync.WaitGroup
	for repo := int64(1); repo <= 16; repo++ {
		workers.Go(func() {
			for i := 0; i < 100; i++ {
				schedule.ListingFailed(repo)
				schedule.WebhookAccepted(repo, at.Add(time.Minute))
				schedule.Announce(repo, i)
				schedule.Announced(repo)
				schedule.Due(repo, at)
				schedule.Known()
				schedule.ListingFinished(repo, at)
			}
		})
	}
	workers.Wait()
	if got := len(schedule.Known()); got != 16 {
		t.Fatalf("known repositories = %d, want 16", got)
	}
	for repo := int64(1); repo <= 16; repo++ {
		snapshot, known := schedule.Snapshot(repo)
		if !known || snapshot.Owed || !snapshot.NextDue.Equal(at.Add(time.Hour)) || !snapshot.LastWebhook.Equal(at.Add(time.Minute)) {
			t.Errorf("repository %d lost schedule state: %+v, known %v", repo, snapshot, known)
		}
		if schedule.Due(repo, at.Add(time.Minute)) {
			t.Errorf("repository %d became due before its own deadline", repo)
		}
	}
}

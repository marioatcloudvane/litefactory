package daemon_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
)

const scheduleInterval = 60 * time.Minute

// scheduledFixture is a fixture whose daemon lists on a 60 minute schedule.
func scheduledFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{DiscoveryInterval: scheduleInterval})
	return f
}

func (f *fixture) listings() int {
	n, _, _, _ := f.forge.counts()
	return n
}

// AC-004: the first heartbeat lists whatever the interval is.
func TestFirstHeartbeatListsAtStart(t *testing.T) {
	f := scheduledFixture(t)
	f.issue("Add a /health endpoint")

	f.settle()

	if got := f.listings(); got != 1 {
		t.Fatalf("first heartbeat made %d issue listings, want 1", got)
	}
}

// AC-005 and AC-007: between listings only the repo listing is skipped, the
// cursor window stays and the next listing is one full interval after the last.
func TestHeartbeatsBetweenListingsSkipOnlyTheRepoListing(t *testing.T) {
	f := scheduledFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")

	first := f.cursor()
	firstRepo, _ := f.repoCursor(f.repo)
	f.clock.advance(30 * time.Second)
	skipped := f.settle()

	if got := f.listings(); got != 1 {
		t.Fatalf("a heartbeat inside the interval listed again: %d listings", got)
	}
	if skipped.Repos != 1 {
		t.Errorf("heartbeat saw %d repos, want 1; local work must still run", skipped.Repos)
	}
	if got := len(f.turns.calls()); got != 0 {
		t.Errorf("ran %d turns without a listing; the issue was never discovered", got)
	}
	after := f.cursor()
	if got, _ := f.repoCursor(f.repo); !got.Equal(firstRepo) {
		t.Errorf("heartbeat-only tick moved the repo cursor to %s from %s", got, firstRepo)
	}
	if !after.LastTickAt.After(first.LastTickAt) {
		t.Error("heartbeat-only tick did not record its completion time")
	}

	// Just short of one interval after the finished listing: still skipped.
	f.clock.advance(scheduleInterval - 2*time.Minute)
	f.settle()
	if got := f.listings(); got != 1 {
		t.Fatalf("listed before a full interval elapsed: %d listings", got)
	}

	f.clock.advance(3 * time.Minute)
	f.settle()
	if got := f.listings(); got != 2 {
		t.Fatalf("no listing one interval after the last: %d listings", got)
	}
	if got, _ := f.repoCursor(f.repo); !got.After(firstRepo) {
		t.Errorf("cursor did not move after a finished listing: %s", got)
	}
}

// AC-006: a failed (rate-limited) listing stays due and is retried on the
// next heartbeat; the cursor does not move.
func TestFailedListingIsRetriedNextHeartbeat(t *testing.T) {
	f := scheduledFixture(t)
	const retryAfter = 45 * time.Second
	f.forge.quotaReset = f.clock.Now().Add(retryAfter).Unix()
	f.issue("Add a /health endpoint")

	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != 1 {
			return false
		}
		writeRateLimit(w, retryAfter)
		return true
	}
	if _, err := f.tickErr(); err == nil {
		t.Fatal("a rate-limited listing returned no error")
	}

	f.clock.advance(2 * retryAfter)
	f.settle()
	if got := f.listings(); got != 2 {
		t.Fatalf("the failed listing was not retried on the next heartbeat: %d listings", got)
	}

	// Once finished, the schedule waits a full interval.
	f.clock.advance(30 * time.Second)
	f.settle()
	if got := f.listings(); got != 2 {
		t.Fatalf("listed again inside the interval after a finished retry: %d listings", got)
	}
}

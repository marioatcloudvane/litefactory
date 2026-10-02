package daemon_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhook"
)

func (f *fixture) listingsIn(repo store.Repo) int {
	return len(f.forge.listingsOf(repo.Owner, repo.Name))
}

func scheduleOf(t *testing.T, d *daemon.Daemon, repo store.Repo) daemon.RepoSchedule {
	t.Helper()
	snap, ok := d.DiscoverySchedule(repo.ID)
	if !ok {
		t.Fatalf("the schedule does not know repo %d", repo.ID)
	}
	return snap
}

// twoRepoWebhookFixture is a webhook fixture with a second registered repository.
func twoRepoWebhookFixture(t *testing.T) (*webhookFixture, store.Repo) {
	t.Helper()
	w := newWebhookFixture(t)
	return w, w.secondRepo()
}

// AC-005: the first heartbeat lists every repository once, and nothing more
// until its own interval.
func TestEveryRepoIsListedOnceAtStart(t *testing.T) {
	f := scheduledFixture(t)
	other := f.secondRepo()

	f.settle()
	if a, b := f.listingsIn(f.repo), f.listingsIn(other); a != 1 || b != 1 {
		t.Fatalf("first heartbeat listed %d and %d times, want 1 and 1", a, b)
	}
	f.clock.advance(30 * time.Second)
	f.settle()
	if a, b := f.listingsIn(f.repo), f.listingsIn(other); a != 1 || b != 1 {
		t.Fatalf("second heartbeat listed again: %d and %d", a, b)
	}
}

// AC-001, AC-002, AC-006, AC-008: a webhook for one repository restarts only its
// countdown (listed at 0, webhook at 50: not due at 60, due at 110), moves no
// cursor, and leaves the neighbour on its own clock.
func TestWebhookPostponesOnlyItsOwnRepoPoll(t *testing.T) {
	w, other := twoRepoWebhookFixture(t)
	w.settle()
	startRepo, ok := w.repoCursor(w.repo)
	if !ok {
		t.Fatal("the first listing wrote no cursor for the repo")
	}
	listedAt := scheduleOf(t, w.daemon, w.repo).LastListing

	w.clock.advance(50 * time.Minute)
	w.daemon.WakeForTest(w.repo.ID, testIssue)
	hookedAt := w.clock.peek()
	w.settle()
	if got, _ := w.repoCursor(w.repo); !got.Equal(startRepo) {
		t.Errorf("a webhook moved the repo cursor from %s to %s", startRepo, got)
	}
	snap := scheduleOf(t, w.daemon, w.repo)
	if !snap.LastWebhook.Equal(hookedAt) || !snap.LastListing.Equal(listedAt) || snap.Owed {
		t.Errorf("snapshot after the webhook = %+v", snap)
	}
	if !snap.NextDue.Equal(hookedAt.Add(scheduleInterval)) {
		t.Errorf("NextDue = %s, want %s", snap.NextDue, hookedAt.Add(scheduleInterval))
	}

	// About 60 minutes in: the neighbour is due, the hooked repo is not.
	w.clock.advance(10 * time.Minute)
	w.settle()
	if got := w.listingsIn(w.repo); got != 1 {
		t.Errorf("the hooked repo listed at 60 minutes: %d listings", got)
	}
	if got := w.listingsIn(other); got != 2 {
		t.Errorf("the neighbour was postponed by another repo's webhook: %d listings", got)
	}

	w.clock.advance(45 * time.Minute)
	w.settle()
	if got := w.listingsIn(w.repo); got != 1 {
		t.Errorf("the hooked repo listed before 110 minutes: %d listings", got)
	}
	w.clock.advance(6 * time.Minute)
	w.settle()
	if got := w.listingsIn(w.repo); got != 2 {
		t.Errorf("the hooked repo was not listed after its restarted interval: %d listings", got)
	}
}

// AC-002: only a delivery that queued a hint postpones; bad signature, ping,
// no-number and duplicate deliveries do not.
func TestRejectedDeliveriesNeverPostponeThePoll(t *testing.T) {
	w := newWebhookFixture(t)
	w.settle()
	stop := w.run()
	secret := []byte(webhookTestSecret)

	sign := func(body []byte) string { return webhook.Sign(secret, body) }
	noNumber := []byte(`{"action":"opened","repository":{"full_name":"` + testOwnerName + `/` + testRepoName + `"}}`)
	ping := []byte(`{"zen":"x","repository":{"full_name":"` + testOwnerName + `/` + testRepoName + `"}}`)
	body := issuePayload(testIssue)

	check := func(what string) {
		t.Helper()
		if snap := scheduleOf(t, w.daemon, w.repo); !snap.LastWebhook.IsZero() {
			t.Fatalf("%s postponed the poll: %+v", what, snap)
		}
	}
	if got := w.post("issues", "d-bad", "sha256="+strings.Repeat("0", 64), body); got != http.StatusUnauthorized {
		t.Fatalf("bad signature answered %d", got)
	}
	check("a bad signature")
	if got := w.post("issues", "d-nosig", "", body); got != http.StatusUnauthorized {
		t.Fatalf("missing signature answered %d", got)
	}
	check("a missing signature")
	if got := w.post("ping", "d-ping", sign(ping), ping); got/100 != 2 {
		t.Fatalf("ping answered %d", got)
	}
	check("a ping")
	if got := w.post("issues", "d-none", sign(noNumber), noNumber); got/100 != 2 {
		t.Fatalf("no-number delivery answered %d", got)
	}
	check("a delivery with no issue number")

	if got := w.post("issues", "d-ok", sign(body), body); got != http.StatusAccepted {
		t.Fatalf("a valid delivery answered %d", got)
	}
	first := scheduleOf(t, w.daemon, w.repo).LastWebhook
	if first.IsZero() {
		t.Fatal("a valid delivery did not record a webhook")
	}
	w.clock.advance(time.Minute)
	if got := w.post("issues", "d-ok", sign(body), body); got != http.StatusOK {
		t.Fatalf("a redelivery answered %d, want 200", got)
	}
	if got := scheduleOf(t, w.daemon, w.repo).LastWebhook; !got.Equal(first) {
		t.Errorf("a duplicate delivery moved the countdown from %s to %s", first, got)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// AC-004: a listing that failed (or was backed off) is owed and retried on the
// next heartbeat, whatever webhooks arrived meanwhile; only a finished listing
// clears it.
func TestOwedListingIsRetriedDespiteWebhooks(t *testing.T) {
	w := newWebhookFixture(t)
	const retryAfter = 45 * time.Second
	w.forge.quotaReset = w.clock.Now().Add(retryAfter).Unix()
	w.forge.onListIssues = func(rw http.ResponseWriter, call int) bool {
		if call != 1 {
			return false
		}
		writeRateLimit(rw, retryAfter)
		return true
	}
	if _, err := w.tickErr(); err == nil {
		t.Fatal("a rate-limited listing returned no error")
	}
	if !scheduleOf(t, w.daemon, w.repo).Owed {
		t.Fatal("a failed listing is not owed")
	}

	// A tick held back by the backoff, then a webhook, change nothing.
	if res, err := w.tickErr(); err != nil || !res.Skipped {
		t.Fatalf("expected a backed-off skip, got %+v, %v", res, err)
	}
	w.daemon.WakeForTest(w.repo.ID, testIssue)
	if !scheduleOf(t, w.daemon, w.repo).Owed {
		t.Fatal("a webhook cleared the owed flag")
	}

	w.clock.advance(2 * retryAfter)
	w.settle()
	if got := w.listings(); got != 2 {
		t.Fatalf("the owed listing was not retried on the next heartbeat: %d listings", got)
	}
	snap := scheduleOf(t, w.daemon, w.repo)
	if snap.Owed || snap.LastListing.IsZero() {
		t.Errorf("snapshot after the retry = %+v", snap)
	}
	if _, ok := w.repoCursor(w.repo); !ok {
		t.Error("the finished retry wrote no cursor")
	}
	w.clock.advance(time.Minute)
	w.settle()
	if got := w.listings(); got != 2 {
		t.Errorf("listed again inside the interval after the retry: %d listings", got)
	}
}

// AC-006: a repo with no cursor row lists from its creation; afterwards each
// listing asks from its own cursor less the five minute overlap.
func TestEachRepoListsFromItsOwnCursor(t *testing.T) {
	f := scheduledFixture(t)
	other := f.secondRepo()

	f.settle()
	for _, repo := range []store.Repo{f.repo, other} {
		since := f.forge.listingsOf(repo.Owner, repo.Name)
		if len(since) != 1 {
			t.Fatalf("%s listed %d times", repo.Name, len(since))
		}
		got, err := time.Parse(time.RFC3339, since[0])
		if err != nil || !got.Equal(repo.CreatedAt.Truncate(time.Second)) {
			t.Errorf("%s first listed since %q (%v), want its creation %s", repo.Name, since[0], err, repo.CreatedAt)
		}
	}

	cursors := map[int64]time.Time{}
	for _, repo := range []store.Repo{f.repo, other} {
		at, ok := f.repoCursor(repo)
		if !ok {
			t.Fatalf("%s has no cursor after its listing finished", repo.Name)
		}
		cursors[repo.ID] = at
	}
	f.clock.advance(scheduleInterval)
	f.settle()
	for _, repo := range []store.Repo{f.repo, other} {
		since := f.forge.listingsOf(repo.Owner, repo.Name)
		want := cursors[repo.ID].Add(-daemon.ListingOverlap).UTC().Format(time.RFC3339)
		if len(since) != 2 || since[1] != want {
			t.Errorf("%s second listing since %v, want %q", repo.Name, since, want)
		}
	}
}

// AC-006: a deferred or failed listing of one repo leaves that repo's cursor
// alone while a neighbour that finished moves its own.
func TestFailedRepoKeepsItsCursorWhileTheNeighbourAdvances(t *testing.T) {
	f := scheduledFixture(t)
	other := f.secondRepo()
	f.settle()
	beforeA, _ := f.repoCursor(f.repo)
	beforeB, _ := f.repoCursor(other)

	f.clock.advance(scheduleInterval)
	const retryAfter = 45 * time.Second
	f.forge.quotaReset = f.clock.Now().Add(retryAfter).Unix()
	f.forge.onListIssues = func(rw http.ResponseWriter, call int) bool {
		// The listings of the second round: the first repo answers, the second is refused.
		if call != 4 {
			return false
		}
		writeRateLimit(rw, retryAfter)
		return true
	}
	if _, err := f.tickErr(); err == nil {
		t.Fatal("a rate-limited listing returned no error")
	}
	if got, _ := f.repoCursor(f.repo); !got.Equal(beforeA) {
		t.Errorf("an aborted tick moved the first repo cursor to %s", got)
	}
	if got, _ := f.repoCursor(other); !got.Equal(beforeB) {
		t.Errorf("the refused repo's cursor moved to %s", got)
	}
	f.clock.advance(2 * retryAfter)
	f.forge.onListIssues = nil
	f.settle()
	for _, repo := range []store.Repo{f.repo, other} {
		if got, _ := f.repoCursor(repo); !got.After(beforeA) {
			t.Errorf("%s cursor did not advance after the retry: %s", repo.Name, got)
		}
	}
}

// Overflow forces every repository's listing, as before the schedule was per repo.
func TestOverflowForcesEveryRepoListing(t *testing.T) {
	w, other := twoRepoWebhookFixture(t)
	w.settle()
	w.clock.advance(30 * time.Second)
	for n := 1; n <= daemon.MaxPendingHintsForTest+1; n++ {
		w.daemon.WakeForTest(w.repo.ID, 1000+n)
	}
	w.settle()
	if a, b := w.listingsIn(w.repo), w.listingsIn(other); a != 2 || b != 2 {
		t.Fatalf("an overflow listed %d and %d times, want 2 and 2", a, b)
	}
	w.clock.advance(30 * time.Second)
	w.settle()
	if a, b := w.listingsIn(w.repo), w.listingsIn(other); a != 2 || b != 2 {
		t.Errorf("the forced listing repeated: %d and %d", a, b)
	}
}

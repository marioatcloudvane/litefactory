package daemon_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
)

func (w *fixture) health() store.HealthSnapshot {
	w.t.Helper()
	snap, err := w.db.ReadHealthSnapshot(w.t.Context())
	if err != nil {
		w.t.Fatalf("read the health snapshot: %v", err)
	}
	return snap
}

func (w *fixture) healthRepo() store.RepoDiscoveryStatus {
	w.t.Helper()
	snap := w.health()
	for _, r := range snap.Repos {
		if r.RepoID == w.repo.ID {
			return r
		}
	}
	w.t.Fatalf("the snapshot has no row for the repository: %+v", snap)
	return store.RepoDiscoveryStatus{}
}

func (w *webhookFixture) registerHook() {
	w.t.Helper()
	if err := w.db.SetRepoWebhook(w.t.Context(), w.repo.ID, 77, "https://example.test/litefactory/webhook", w.clock.peek()); err != nil {
		w.t.Fatalf("record the hook: %v", err)
	}
}

// AC-001, AC-003: the first heartbeat publishes the listener and the
// repository's state; with no hook and nothing delivered it is polling only.
func TestHealthSnapshotIsPublishedByTheFirstHeartbeat(t *testing.T) {
	w := newWebhookFixture(t)
	if snap := w.health(); snap.Found {
		t.Fatal("a snapshot exists before the daemon ticked")
	}

	w.plantCursor()

	snap := w.health()
	if !snap.Found || !snap.Listener.Listening || snap.Listener.ListenAddress != w.ln.Addr().String() {
		t.Fatalf("listener = %+v, want bound at %s", snap.Listener, w.ln.Addr())
	}
	if snap.Listener.DaemonStartedAt.IsZero() || snap.Listener.WrittenAt.IsZero() {
		t.Errorf("listener times missing: %+v", snap.Listener)
	}
	r := w.healthRepo()
	if r.WebhookMode != daemon.ModePollingOnly {
		t.Errorf("mode = %q, want %q", r.WebhookMode, daemon.ModePollingOnly)
	}
	if !r.SecretPresent {
		t.Error("the keychain holds the secret but the snapshot says it is missing")
	}
	if !r.LastDeliveryAt.IsZero() {
		t.Errorf("last delivery = %s before any delivery", r.LastDeliveryAt)
	}
	if r.LastPollFinishedAt.IsZero() || !r.NextPollDueAt.Equal(r.LastPollFinishedAt.Add(scheduleInterval)) || r.PollOwed {
		t.Errorf("poll state = finished %s next %s owed %v", r.LastPollFinishedAt, r.NextPollDueAt, r.PollOwed)
	}
}

// AC-002: a daemon without a listener reports it off and every repository as
// polling only; the secret is not a concern.
func TestHealthSnapshotWithTheListenerOff(t *testing.T) {
	f := scheduledFixture(t)
	f.plantCursor()

	snap := f.health()
	if !snap.Found || snap.Listener.Listening || snap.Listener.ListenAddress != "" {
		t.Fatalf("listener = %+v, want off", snap.Listener)
	}
	if r := f.healthRepo(); r.WebhookMode != daemon.ModePollingOnly || r.SecretPresent {
		t.Errorf("row = %+v", r)
	}
}

// AC-003, AC-004: registered hook, then a delivery, change the mode word and
// the last-delivery time on the heartbeat that follows.
func TestHealthModeFollowsHookAndDelivery(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()

	w.registerHook()
	w.clock.advance(30 * time.Second)
	w.settle()
	if r := w.healthRepo(); r.WebhookMode != daemon.ModeWebhookIdle {
		t.Fatalf("mode = %q with a hook and no delivery, want %q", r.WebhookMode, daemon.ModeWebhookIdle)
	}

	w.daemon.WakeForTest(w.repo.ID, testIssue)
	w.clock.advance(30 * time.Second)
	w.settle()
	r := w.healthRepo()
	if r.WebhookMode != daemon.ModeWebhook || r.LastDeliveryAt.IsZero() {
		t.Fatalf("row after a delivery = %+v", r)
	}
	// Delivery telemetry advances while the deadline stays anchored to the listing.
	if !r.NextPollDueAt.Equal(r.LastPollFinishedAt.Add(scheduleInterval)) {
		t.Errorf("next poll %s, want one interval after the listing %s", r.NextPollDueAt, r.LastPollFinishedAt)
	}
}

// AC-005: a repository whose secret is not in the keychain reports it.
func TestHealthReportsAMissingSecret(t *testing.T) {
	w := newWebhookFixture(t)
	w.spy.mu.Lock()
	clear(w.spy.secrets)
	w.spy.mu.Unlock()
	w.plantCursor()

	if r := w.healthRepo(); r.SecretPresent {
		t.Fatal("secret reported present although the keychain has none")
	}
	w.spy.store(testOwnerName+"/"+testRepoName, webhookTestSecret)
	w.clock.advance(6 * time.Minute) // past the five minute retry
	w.settle()
	if r := w.healthRepo(); !r.SecretPresent {
		t.Error("the stored secret was not picked up and published")
	}
}

// AC-006: a heartbeat in which nothing changed writes nothing.
func TestHealthIsWrittenOnlyWhenAValueChanged(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	first := w.health().Listener.WrittenAt

	w.clock.advance(30 * time.Second)
	w.settle()
	if got := w.health().Listener.WrittenAt; !got.Equal(first) {
		t.Fatalf("an unchanged heartbeat rewrote the snapshot: %s then %s", first, got)
	}

	w.registerHook()
	w.clock.advance(30 * time.Second)
	w.settle()
	if got := w.health().Listener.WrittenAt; !got.After(first) {
		t.Errorf("a changed mode did not rewrite the snapshot: %s", got)
	}
}

// AC-007: a new daemon's snapshot starts "since start": the previous run's
// delivery is not carried over.
func TestHealthResetsAtStart(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.daemon.WakeForTest(w.repo.ID, testIssue)
	w.clock.advance(30 * time.Second)
	w.settle()
	if w.healthRepo().LastDeliveryAt.IsZero() {
		t.Fatal("setup: the delivery was not published")
	}

	replacement := w.newDaemon(daemon.Options{DiscoveryInterval: scheduleInterval})
	w.daemon = replacement
	w.clock.advance(30 * time.Second)
	w.settle()
	if r := w.healthRepo(); !r.LastDeliveryAt.IsZero() {
		t.Errorf("a restarted daemon kept the old delivery time %s", r.LastDeliveryAt)
	}
}

// AC-006: a failed write never fails the tick and is retried on the next
// heartbeat although nothing changed.
func TestHealthWriteFailureIsLoggedAndRetried(t *testing.T) {
	f := scheduledFixture(t)
	raw, err := sql.Open("sqlite", store.DSN(f.db.Path()))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(t.Context(), `ALTER TABLE repo_discovery_status RENAME TO repo_discovery_status_off`); err != nil {
		t.Fatal(err)
	}

	f.plantCursor() // fails to publish; must not fail the tick
	if !strings.Contains(f.logs.String(), "health snapshot not published") {
		t.Fatalf("the failed write was not logged:\n%s", f.logs)
	}
	if f.health().Found {
		t.Fatal("a snapshot exists although the write failed")
	}

	if _, err := raw.ExecContext(t.Context(), `ALTER TABLE repo_discovery_status_off RENAME TO repo_discovery_status`); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(30 * time.Second)
	f.settle()
	if !f.health().Found {
		t.Error("the unchanged snapshot was not retried after a failed write")
	}
}

func (w *webhookFixture) unannounced() []map[string]any {
	return dispatchRecords(w.t, w.logs, "poll found a change no webhook announced; check the webhook path")
}

// AC-008: an issue updated after the previous listing started, with no hint,
// is reported once for a webhook-mode repository, with the others counted.
func TestUnannouncedChangeWarnsOncePerListing(t *testing.T) {
	w := newWebhookFixture(t)
	w.registerHook()
	w.plantCursor()

	for _, n := range []int{testIssue, testIssue + 1, testIssue + 2} {
		w.forge.addIssue(fakeIssue{Number: n, Title: "x", Body: "y", User: stranger()})
	}
	w.clock.advance(scheduleInterval + time.Minute)
	w.settle()

	got := w.unannounced()
	if len(got) != 1 {
		t.Fatalf("%d warnings, want exactly one:\n%s", len(got), w.logs)
	}
	rec := got[0]
	if fmt.Sprint(rec["issue"]) != fmt.Sprint(testIssue) || fmt.Sprint(rec["others"]) != "2" || fmt.Sprint(rec["repo"]) != testOwnerName+"/"+testRepoName {
		t.Errorf("warning = %v, want repo, issue %d and 2 others", rec, testIssue)
	}
}

// AC-008: a hint announces its issue until the listing that follows finishes;
// the set is then cleared.
func TestAnnouncedIssueDoesNotWarnAndIsClearedAfterTheListing(t *testing.T) {
	w := newWebhookFixture(t)
	w.registerHook()
	w.plantCursor()

	w.forge.addIssue(fakeIssue{Number: testIssue, Title: "x", Body: "y", User: stranger()})
	w.daemon.WakeForTest(w.repo.ID, testIssue)
	w.clock.advance(scheduleInterval + time.Minute)
	w.settle()
	if got := w.unannounced(); len(got) != 0 {
		t.Fatalf("an announced change warned: %v", got)
	}

	w.forge.staleIssue(testIssue, w.clock.peek())
	w.clock.advance(scheduleInterval + time.Minute)
	w.settle()
	if got := w.unannounced(); len(got) != 1 {
		t.Fatalf("%d warnings for a change after the set was cleared, want 1:\n%s", len(got), w.logs)
	}
}

// AC-008: overlap re-sightings, polling-only repositories and a listing with no
// earlier listing in this run never warn.
func TestUnannouncedChangeIgnoresOverlapPollingOnlyAndFirstListing(t *testing.T) {
	// Overlap: updated before the previous listing started.
	w := newWebhookFixture(t)
	w.registerHook()
	w.forge.addIssue(fakeIssue{Number: testIssue, Title: "x", Body: "y", User: stranger()})
	w.plantCursor() // lists everything from registration; nothing earlier in this run
	if got := w.unannounced(); len(got) != 0 {
		t.Fatalf("the first listing of the run warned: %v", got)
	}
	w.forge.staleIssue(testIssue, w.clock.peek().Add(-time.Minute))
	w.clock.advance(scheduleInterval + time.Minute)
	w.settle()
	if got := w.unannounced(); len(got) != 0 {
		t.Fatalf("an overlap re-sighting warned: %v", got)
	}

	// Polling only: no hook registered, nothing delivered.
	p := newWebhookFixture(t)
	p.plantCursor()
	p.forge.addIssue(fakeIssue{Number: testIssue, Title: "x", Body: "y", User: stranger()})
	p.clock.advance(scheduleInterval + time.Minute)
	p.settle()
	if got := p.unannounced(); len(got) != 0 {
		t.Fatalf("a polling-only repository warned: %v", got)
	}

	// No listener at all.
	f := scheduledFixture(t)
	f.plantCursor()
	f.issue("x")
	f.clock.advance(scheduleInterval + time.Minute)
	f.settle()
	if strings.Contains(f.logs.String(), "no webhook announced") {
		t.Fatalf("a daemon without a listener warned:\n%s", f.logs)
	}
}

package store

import (
	"testing"
	"time"
)

func TestHealthSnapshotRoundTripAndReplace(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	a := testRepo(t, db, "acme", "api")
	b := testRepo(t, db, "acme", "web")
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	snap, err := db.ReadHealthSnapshot(ctx)
	if err != nil || snap.Found {
		t.Fatalf("empty database: found=%v err=%v", snap.Found, err)
	}

	listener := ListenerStatus{ListenAddress: "127.0.0.1:8787", Listening: true, DaemonStartedAt: at, WrittenAt: at.Add(time.Minute)}
	rows := []RepoDiscoveryStatus{
		{RepoID: b.ID, WebhookMode: "polling only", WrittenAt: at},
		{RepoID: a.ID, LastDeliveryAt: at.Add(time.Second), LastPollFinishedAt: at.Add(2 * time.Second), NextPollDueAt: at.Add(time.Hour),
			PollOwed: true, SecretPresent: true, WebhookMode: "webhook", WrittenAt: at},
	}
	if err := db.ReplaceHealthSnapshot(ctx, listener, rows); err != nil {
		t.Fatal(err)
	}
	snap, err = db.ReadHealthSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Found || snap.Listener != listener {
		t.Fatalf("listener = %+v found=%v", snap.Listener, snap.Found)
	}
	if len(snap.Repos) != 2 || snap.Repos[0].Name != "api" || snap.Repos[1].Name != "web" {
		t.Fatalf("repos = %+v", snap.Repos)
	}
	got := snap.Repos[0]
	if got.Owner != "acme" || !got.LastDeliveryAt.Equal(at.Add(time.Second)) || !got.NextPollDueAt.Equal(at.Add(time.Hour)) ||
		!got.PollOwed || !got.SecretPresent || got.WebhookMode != "webhook" {
		t.Errorf("api row = %+v", got)
	}
	if w := snap.Repos[1]; !w.LastDeliveryAt.IsZero() || !w.LastPollFinishedAt.IsZero() || !w.NextPollDueAt.IsZero() || w.PollOwed || w.SecretPresent {
		t.Errorf("web row should hold nulls and false: %+v", w)
	}

	// Replacing drops the repository that is no longer in the snapshot and
	// keeps a single listener row.
	listener.Listening, listener.WrittenAt = false, at.Add(time.Hour)
	if err := db.ReplaceHealthSnapshot(ctx, listener, rows[1:]); err != nil {
		t.Fatal(err)
	}
	snap, err = db.ReadHealthSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Repos) != 1 || snap.Repos[0].RepoID != a.ID || snap.Listener.Listening {
		t.Fatalf("after replace: %+v", snap)
	}
}

func TestHealthSnapshotReplaceIsAtomic(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	a := testRepo(t, db, "acme", "api")
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	listener := ListenerStatus{DaemonStartedAt: at, WrittenAt: at}
	good := []RepoDiscoveryStatus{{RepoID: a.ID, WebhookMode: "polling only", WrittenAt: at}}
	if err := db.ReplaceHealthSnapshot(ctx, listener, good); err != nil {
		t.Fatal(err)
	}
	// A repository that does not exist violates the foreign key; nothing of the
	// failed replacement may remain.
	bad := []RepoDiscoveryStatus{{RepoID: a.ID, WebhookMode: "webhook", WrittenAt: at}, {RepoID: 9999, WebhookMode: "webhook", WrittenAt: at}}
	listener.ListenAddress = "changed"
	if err := db.ReplaceHealthSnapshot(ctx, listener, bad); err == nil {
		t.Fatal("expected a foreign key failure")
	}
	snap, err := db.ReadHealthSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Listener.ListenAddress != "" || len(snap.Repos) != 1 || snap.Repos[0].WebhookMode != "polling only" {
		t.Fatalf("failed replace leaked: %+v", snap)
	}
}

func TestHealthSnapshotRowsFollowRepoDeletion(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	a := testRepo(t, db, "acme", "api")
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := db.ReplaceHealthSnapshot(ctx, ListenerStatus{DaemonStartedAt: at, WrittenAt: at},
		[]RepoDiscoveryStatus{{RepoID: a.ID, WebhookMode: "polling only", WrittenAt: at}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM repos WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := db.ReadHealthSnapshot(ctx)
	if err != nil || len(snap.Repos) != 0 {
		t.Fatalf("repos = %+v err=%v", snap.Repos, err)
	}
}

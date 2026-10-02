package store

import (
	"errors"
	"testing"
	"time"
)

func TestRepoWebhookRoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	other := testRepo(t, db, "acme", "web")
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	if _, err := db.GetRepoWebhook(t.Context(), repo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get before set: %v, want ErrNotFound", err)
	}
	if err := db.SetRepoWebhook(t.Context(), repo.ID, 42, "https://h/x", at); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetRepoWebhook(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoID != repo.ID || got.HookID != 42 || got.URL != "https://h/x" || !got.UpdatedAt.Equal(at) {
		t.Fatalf("got %+v", got)
	}

	if err := db.SetRepoWebhook(t.Context(), repo.ID, 43, "https://h/y", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetRepoWebhook(t.Context(), repo.ID); got.HookID != 43 || got.URL != "https://h/y" {
		t.Fatalf("replace did not take: %+v", got)
	}
	if _, err := db.GetRepoWebhook(t.Context(), other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another repo has a record: %v", err)
	}

	if err := db.ClearRepoWebhook(t.Context(), repo.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearRepoWebhook(t.Context(), repo.ID); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
	if _, err := db.GetRepoWebhook(t.Context(), repo.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after clear: %v", err)
	}
}

func TestRepoWebhookRefusesABadHookID(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	if err := db.SetRepoWebhook(t.Context(), repo.ID, 0, "https://h", time.Now()); err == nil {
		t.Error("hook id 0 was accepted")
	}
	if err := db.SetRepoWebhook(t.Context(), 9999, 1, "https://h", time.Now()); err == nil {
		t.Error("an unknown repo was accepted")
	}
}

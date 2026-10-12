package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func resolvedSessionTicket(container, remoteID string) ticket.Ref {
	return ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: remoteID}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: "https://api.github.com", Container: container, Key: "7"}}
}

func TestSessionTicketBindingPreservesLegacySessionAndConsumption(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	session, err = db.FreezeSessionSlug(t.Context(), session.ID, "frozen-name", "lf/7-frozen-name")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.BootstrapWatermark(t.Context(), session.ID, 123); err != nil {
		t.Fatal(err)
	}
	watermark, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindLegacySessionOrigin(t.Context(), session.ID, "https://api.github.com")
	if err != nil {
		t.Fatal(err)
	}
	if binding.RemoteID != "" || binding.TaskRef != session.TaskRef {
		t.Fatalf("legacy binding: %+v", binding)
	}
	if _, err := binding.Ref(); err == nil {
		t.Fatal("unresolved locator authorized mutation")
	}
	ref := resolvedSessionTicket("owner/repo", "700")
	binding, err = db.BindSessionTicket(t.Context(), session.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := binding.Ref()
	if err != nil || !got.Equal(ref) {
		t.Fatalf("verified binding: %+v, %v", binding, err)
	}
	// Replays and migrations must not touch the original naming or consumption.
	if _, err := db.BindSessionTicket(t.Context(), session.ID, ref); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterWatermark, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, session) || !reflect.DeepEqual(afterWatermark, watermark) {
		t.Fatal("binding changed frozen history or watermark")
	}
}

func TestSessionTicketBindingRefusesConflictsAtomically(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	first, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	original := resolvedSessionTicket("owner/repo", "700")
	if _, err := db.BindSessionTicket(t.Context(), first.ID, original); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ticket.Ref)
	}{
		{"remote ID", func(ref *ticket.Ref) { ref.Identity.RemoteID = "701" }},
		{"origin", func(ref *ticket.Ref) {
			ref.Identity.Origin = "https://enterprise.example/api/v3"
			ref.Locator.Origin = ref.Identity.Origin
		}},
		{"container", func(ref *ticket.Ref) { ref.Locator.Container = "other/repo" }},
		{"number", func(ref *ticket.Ref) { ref.Locator.Key = "8" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := original
			tc.change(&changed)
			if _, err := db.BindSessionTicket(t.Context(), first.ID, changed); !errors.Is(err, ErrTicketIdentityConflict) {
				t.Fatalf("accepted conflicting binding: %v", err)
			}
			stored, err := db.GetSessionTicket(t.Context(), first.ID)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := stored.Ref()
			if err != nil || ref != original {
				t.Fatalf("conflict altered existing mapping: %+v, %v", ref, err)
			}
		})
	}
	otherRepo := testRepo(t, db, "owner", "other")
	other, err := db.GetOrCreateSession(t.Context(), otherRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := resolvedSessionTicket("owner/other", "700")
	if _, err := db.BindSessionTicket(t.Context(), other.ID, duplicate); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("canonical ticket became two sessions: %v", err)
	}
	if _, err := db.GetSessionTicket(t.Context(), other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed binding left partial metadata: %v", err)
	}
	duplicate.Identity.Origin = "https://enterprise.example/api/v3"
	duplicate.Locator.Origin = duplicate.Identity.Origin
	if _, err := db.BindSessionTicket(t.Context(), other.ID, duplicate); err != nil {
		t.Fatalf("unrelated origins collide: %v", err)
	}
}

func TestSessionTicketOriginSurvivesReopenAndCannotRebind(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BindLegacySessionOrigin(t.Context(), session.ID, "https://api.github.com"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BindLegacySessionOrigin(t.Context(), session.ID, "https://enterprise.example/api/v3"); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("restart silently rebound origin: %v", err)
	}
	saved, err := db.GetSessionTicket(t.Context(), session.ID)
	if err != nil || saved.Locator.Origin != "https://api.github.com" || saved.RemoteID != "" {
		t.Fatalf("restart lost unresolved origin: %+v, %v", saved, err)
	}
}

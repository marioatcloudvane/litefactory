package store

import (
	"errors"
	"testing"
	"time"
)

func TestDB_ForgetArchivedFamily(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	t.Run("nothing left/deletes skeleton and returns counts", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		other := archiveTestSession(t, db, "acme", 8)
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}

		deleted, err := db.ForgetArchivedFamily(t.Context(), f, now)
		if err != nil {
			t.Fatal(err)
		}
		if deleted["sessions"] != 1 {
			t.Fatalf("deleted = %v", deleted)
		}
		if _, err := db.GetSession(t.Context(), s.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("session after forget: %v", err)
		}
		if _, err := db.GetSession(t.Context(), other.ID); err != nil {
			t.Fatalf("other session removed: %v", err)
		}
	})

	t.Run("archived rows left/returns rows left and keeps skeleton", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		archiveTestComments(t, db, s.ID, 2)
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		deleted, err := db.ForgetArchivedFamily(t.Context(), f, now)
		if !errors.Is(err, ErrArchiveRowsLeft) || deleted != nil {
			t.Fatalf("deleted=%v err=%v", deleted, err)
		}
		if _, err := db.GetSession(t.Context(), s.ID); err != nil {
			t.Fatalf("session removed: %v", err)
		}
	})

	t.Run("plan rows left/returns rows left", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		planSeed(t, db, s.TaskRef, "applied")
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		if _, err := db.ForgetArchivedFamily(t.Context(), f, now); !errors.Is(err, ErrArchiveRowsLeft) {
			t.Fatalf("err = %v", err)
		}
		if n := planCount(t, db, "plan_story_effects", s.TaskRef); n != 1 {
			t.Fatalf("plan effects = %d", n)
		}
	})

	t.Run("pending plan effect/returns not settled", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		planSeed(t, db, s.TaskRef, "pending")
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		if _, err := db.ForgetArchivedFamily(t.Context(), f, now); !errors.Is(err, ErrArchiveNotSettled) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("unsettled retirement intent/returns not settled", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO retirement_intents(session_id,sweep_id,kind,resource,expected_sha,base_sha,action,observed_at) VALUES(?,?,?,?,?,?,?,?)`,
			s.ID, "sweep", "worktree", "/x", "a", "b", "remove", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		deleted, err := db.ForgetArchivedFamily(t.Context(), f, now)
		if !errors.Is(err, ErrArchiveNotSettled) || deleted != nil {
			t.Fatalf("deleted=%v err=%v", deleted, err)
		}
		if _, err := db.GetSession(t.Context(), s.ID); err != nil {
			t.Fatalf("session removed: %v", err)
		}
	})

	t.Run("unknown session/returns not found", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		f := ArchiveFamily{SessionID: 999, RepoID: 1, TaskRef: "github:x/y#1"}
		if _, err := db.ForgetArchivedFamily(t.Context(), f, now); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPlaceholders(t *testing.T) {
	t.Parallel()
	for n, want := range map[int]string{0: "", 1: "?", 3: "?,?,?"} {
		if got := placeholders(n); got != want {
			t.Errorf("placeholders(%d) = %q, want %q", n, got, want)
		}
	}
}

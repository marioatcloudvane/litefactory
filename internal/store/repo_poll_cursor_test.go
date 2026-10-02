package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRepoPollCursorIsForwardOnly(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")

	if _, ok, err := db.GetRepoPollCursor(t.Context(), repo.ID); err != nil || ok {
		t.Fatalf("a repo that never listed has a cursor: ok=%v err=%v", ok, err)
	}
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := db.SetRepoPollCursor(t.Context(), repo.ID, t0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := db.SetRepoPollCursor(t.Context(), repo.ID, t0.Add(-time.Hour)); err != nil {
		t.Fatalf("set older: %v", err)
	}
	if got, ok, err := db.GetRepoPollCursor(t.Context(), repo.ID); err != nil || !ok || !got.Equal(t0) {
		t.Fatalf("an older write moved the cursor: %s ok=%v err=%v", got, ok, err)
	}
	if err := db.SetRepoPollCursor(t.Context(), repo.ID, t0.Add(time.Minute)); err != nil {
		t.Fatalf("set newer: %v", err)
	}
	if got, _, _ := db.GetRepoPollCursor(t.Context(), repo.ID); !got.Equal(t0.Add(time.Minute)) {
		t.Fatalf("a newer write did not move the cursor: %s", got)
	}
	if err := db.SetRepoPollCursor(t.Context(), repo.ID, time.Time{}); err == nil {
		t.Error("a zero time was written")
	}
}

func TestRepoPollCursorsAreIndependent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	a := testRepo(t, db, "acme", "api")
	b := testRepo(t, db, "acme", "web")
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := db.SetRepoPollCursor(t.Context(), a.ID, t0); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.GetRepoPollCursor(t.Context(), b.ID); ok {
		t.Error("writing one repo's cursor created another's")
	}
}

func TestRepoPollCursorMigrationSeedsEveryExistingRepoFromTheGlobalCursor(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	applyMigrationsThrough(t, db, migrationVersionBefore(t, "repo_poll_cursors"))
	a := testRepo(t, db, "acme", "api")
	b := testRepo(t, db, "acme", "web")
	global := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if _, err := db.InitPollCursor(t.Context(), global); err != nil {
		t.Fatalf("plant the global cursor: %v", err)
	}

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, repo := range []Repo{a, b} {
		got, ok, err := db.GetRepoPollCursor(t.Context(), repo.ID)
		if err != nil || !ok || !got.Equal(global) {
			t.Errorf("repo %d seeded %s ok=%v err=%v, want %s", repo.ID, got, ok, err, global)
		}
	}
	// A repo registered afterwards has no row and so lists from its creation.
	c := testRepo(t, db, "acme", "docs")
	if _, ok, _ := db.GetRepoPollCursor(t.Context(), c.ID); ok {
		t.Error("a repo registered after the migration has a cursor row")
	}
	// The global row keeps its value and last_tick_at.
	if cursor, _ := db.GetPollCursor(t.Context()); !cursor.TasksUpdatedSince.Equal(global) {
		t.Errorf("global cursor changed to %s", cursor.TasksUpdatedSince)
	}
}

func TestRepoPollCursorMigrationWithoutAGlobalCursorSeedsNothing(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "repo_poll_cursors"))
	repo := testRepo(t, db, "acme", "api")
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, ok, _ := db.GetRepoPollCursor(t.Context(), repo.ID); ok {
		t.Error("a database that never ticked was seeded")
	}
}

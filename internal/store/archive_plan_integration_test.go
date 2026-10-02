package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchivePlanIntegrationForgetRefusesWhilePlanRowsRemain(t *testing.T) {
	db := newTestDB(t)
	f := archiveITSeed(t, db)
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES(?,?,1)`, f.TaskRef, "US-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ForgetArchivedFamily(t.Context(), f, time.Now()); !errors.Is(err, ErrArchiveRowsLeft) {
		t.Fatalf("err %v, want ErrArchiveRowsLeft", err)
	}
	var n int64
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sessions WHERE id=?`, f.SessionID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("skeleton touched: %d %v", n, err)
	}
	// Without a grant the protective trigger refuses the delete.
	_, err := db.sql.ExecContext(t.Context(), `DELETE FROM plan_story_drafts`)
	if err == nil || !strings.Contains(err.Error(), "reserved Plan ordinal") {
		t.Fatalf("ungranted delete: %v", err)
	}
}

func TestArchivePlanIntegrationStaleGrantsPurgedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "litefactory.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES('p','k',1)`); err != nil {
		t.Fatal(err)
	}
	// A committed grant (crash or bypass) would unprotect the history.
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO plan_archive_grants(parent) VALUES('p')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var n int64
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM plan_archive_grants`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("grants after reopen: %d %v", n, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM plan_story_drafts`); err == nil {
		t.Fatal("history deletable after stale grant purge")
	}
}

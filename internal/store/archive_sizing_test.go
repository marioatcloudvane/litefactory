package store

import (
	"testing"
	"time"
)

func TestStorySizingPublicationSurvivesArchiveUntilFamilyForgotten(t *testing.T) {
	db := newTestDB(t)
	family := archiveITSeed(t, db)
	other := archiveTestSession(t, db, "other", 7)
	for _, parent := range []string{family.TaskRef, other.TaskRef} {
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO published_story_sizing(parent_task,generation,story_key,projection_revision,facts_json) VALUES(?,1,'story-001','projection','{}')`, parent); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if _, err := db.DeleteArchivedFamily(t.Context(), family, archiveITTrailer(t, db, family), now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM published_story_sizing`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("archive removed retained provenance: count=%d err=%v", count, err)
	}
	deleted, err := db.ForgetArchivedFamily(t.Context(), family, now)
	if err != nil || deleted["published_story_sizing"] != 1 {
		t.Fatalf("forget provenance: deleted=%v err=%v", deleted, err)
	}
	var parent string
	if err = db.sql.QueryRowContext(t.Context(), `SELECT parent_task FROM published_story_sizing`).Scan(&parent); err != nil || parent != other.TaskRef {
		t.Fatalf("unrelated provenance changed: parent=%q err=%v", parent, err)
	}
}

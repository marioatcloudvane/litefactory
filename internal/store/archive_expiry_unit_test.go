package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// expirySeedRun inserts a run for the session; finished "" leaves it unfinished.
func expirySeedRun(t *testing.T, db *DB, sessionID int64, segmentID int64, ordinal int, finished string) {
	t.Helper()
	var fin any
	if finished != "" {
		fin = finished
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO runs
		(session_id, segment_id, ordinal, mode, agent, trigger_comment_id, status, started_at, finished_at, log_path)
		VALUES (?, ?, ?, 'research', 'claude', ?, 'succeeded', '2026-01-01T00:00:00.000Z', ?, 'x.log')`,
		sessionID, segmentID, ordinal, ordinal+1000, fin); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func expirySegment(t *testing.T, db *DB, s Session) int64 {
	t.Helper()
	seg, err := db.GetOrCreateSegment(t.Context(), s.ID, FirstSegmentOrdinal, "seg-"+s.TaskRef, AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	return seg.ID
}

func TestDB_ListArchivedRunArtefacts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	other := testRepo(t, db, "acme", "other")
	mk := func(r Repo, issue int) Session {
		s, err := db.GetOrCreateSession(ctx, r, issue)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	owner := mk(repo, 10)
	child := mk(repo, 11)
	unrelated := mk(repo, 12)
	sameIssueOtherRepo := mk(other, 11)

	if _, err := db.sql.ExecContext(ctx, `INSERT INTO managed_features(parent_task, generation, state, ready) VALUES (?, 1, 'implementing', 1)`, owner.TaskRef); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO managed_stories(parent_task, generation, story_key, issue_number, issue_id, body_hash, dependencies, state) VALUES (?, 1, 'STORY-001', 11, 111, 'h', '[]', 'completed')`, owner.TaskRef); err != nil {
		t.Fatal(err)
	}

	archiveDate := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return FormatTime(archiveDate.Add(d)) }

	oseg, cseg, useg, xseg := expirySegment(t, db, owner), expirySegment(t, db, child), expirySegment(t, db, unrelated), expirySegment(t, db, sameIssueOtherRepo)
	expirySeedRun(t, db, owner.ID, oseg, 1, at(-time.Hour))
	expirySeedRun(t, db, owner.ID, oseg, 2, at(0))           // exactly at archive date: included
	expirySeedRun(t, db, owner.ID, oseg, 3, at(time.Second)) // after: excluded
	expirySeedRun(t, db, owner.ID, oseg, 4, "")              // unfinished: excluded
	expirySeedRun(t, db, child.ID, cseg, 1, at(-2*time.Hour))
	expirySeedRun(t, db, child.ID, cseg, 2, "")
	expirySeedRun(t, db, unrelated.ID, useg, 1, at(-time.Hour))
	expirySeedRun(t, db, sameIssueOtherRepo.ID, xseg, 1, at(-time.Hour))

	t.Run("family with child story/returns finished runs at or before date", func(t *testing.T) {
		got, found, err := db.ListArchivedRunArtefacts(ctx, owner.TaskRef, archiveDate)
		if err != nil || !found {
			t.Fatalf("found=%v err=%v", found, err)
		}
		want := []ArchivedRun{{Issue: 10, Ordinal: 1}, {Issue: 10, Ordinal: 2}, {Issue: 11, Ordinal: 1}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("later archive date/includes later finished run but never unfinished", func(t *testing.T) {
		got, _, err := db.ListArchivedRunArtefacts(ctx, owner.TaskRef, archiveDate.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		want := []ArchivedRun{{10, 1}, {10, 2}, {10, 3}, {11, 1}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("early archive date/returns none but found", func(t *testing.T) {
		got, found, err := db.ListArchivedRunArtefacts(ctx, owner.TaskRef, archiveDate.Add(-24*time.Hour))
		if err != nil || !found || len(got) != 0 {
			t.Errorf("got %v found=%v err=%v", got, found, err)
		}
	})
	t.Run("unknown task ref/returns found false", func(t *testing.T) {
		got, found, err := db.ListArchivedRunArtefacts(ctx, "github:acme/api#999", archiveDate)
		if err != nil || found || len(got) != 0 {
			t.Errorf("got %v found=%v err=%v", got, found, err)
		}
	})
	t.Run("child session without managed story parent/is own family only", func(t *testing.T) {
		got, found, err := db.ListArchivedRunArtefacts(ctx, unrelated.TaskRef, archiveDate)
		if err != nil || !found || !reflect.DeepEqual(got, []ArchivedRun{{12, 1}}) {
			t.Errorf("got %v found=%v err=%v", got, found, err)
		}
	})
	t.Run("cancelled context/returns error", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, _, err := db.ListArchivedRunArtefacts(c, owner.TaskRef, archiveDate); err == nil {
			t.Error("want error")
		}
	})
}

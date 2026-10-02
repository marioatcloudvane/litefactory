package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
)

func archiveTestSession(t *testing.T, db *DB, owner string, issue int) Session {
	t.Helper()
	repo := testRepo(t, db, owner, "api")
	s, err := db.GetOrCreateSession(t.Context(), repo, issue)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return s
}

func archiveTestComments(t *testing.T, db *DB, sessionID int64, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO comments(session_id,forge_comment_id,author_handle,author_is_bot,body,created_at,classification,recorded_at) VALUES(?,?,?,?,?,?,?,?)`,
			sessionID, i, "alice", 0, "body\xff", "2026-01-01T00:00:00Z", "instruction", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
	}
}

func archiveTestCount(t *testing.T, db *DB, table string, sessionID int64) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table+` WHERE session_id=?`, sessionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func archiveTestTrailer(t *testing.T, db *DB, f ArchiveFamily) archive.Trailer {
	t.Helper()
	d := archive.NewDigester()
	if err := db.StreamArchiveRows(t.Context(), f, func(r archive.Row) error { _, err := d.Add(r); return err }); err != nil {
		t.Fatalf("stream: %v", err)
	}
	return d.Trailer()
}

func TestArchiveRegistryProblems_MigratedDatabaseIsClean(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	problems, err := db.ArchiveRegistryProblems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("registry problems: %v", problems)
	}
}

func TestArchiveRegistry_ReturnsCopyWithEveryClass(t *testing.T) {
	t.Parallel()
	reg := ArchiveRegistry()
	seen := map[string]bool{}
	classes := map[ArchiveClass]bool{}
	for _, e := range reg {
		if seen[e.Name] {
			t.Fatalf("%s listed twice", e.Name)
		}
		seen[e.Name] = true
		classes[e.Class] = true
		if (e.Class == ArchiveClassArchived) != (e.Where != "") {
			t.Fatalf("%s class %s with selector %q", e.Name, e.Class, e.Where)
		}
	}
	for _, c := range []ArchiveClass{ArchiveClassArchived, ArchiveClassSkeleton, ArchiveClassGlobal} {
		if !classes[c] {
			t.Errorf("no table classified %s", c)
		}
	}
	if classes[ArchiveClassStoryTwoOwned] {
		t.Error("Plan tables moved to the archived class; none may stay story-002-owned")
	}
	reg[0].Name = "mutated"
	if ArchiveRegistry()[0].Name == "mutated" {
		t.Fatal("ArchiveRegistry exposes internal slice")
	}
}

func TestDB_GetArchiveOwner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		prepare     func(t *testing.T, db *DB, s Session)
		wantPending bool
		wantReason  string
		wantRows    int64
	}{
		{"settled with rows/counts archivable rows", func(t *testing.T, db *DB, s Session) { archiveTestComments(t, db, s.ID, 2) }, false, "", 2},
		{"settled without rows/nothing to archive", func(*testing.T, *DB, Session) {}, false, "", 0},
		{"unknown retirement intent/pending", func(t *testing.T, db *DB, s Session) {
			archiveTestComments(t, db, s.ID, 1)
			if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO retirement_intents(session_id,sweep_id,kind,resource,expected_sha,base_sha,action,observed_at) VALUES(?,?,?,?,?,?,?,?)`,
				s.ID, "sweep", "worktree", "/x", "a", "b", "remove", "2026-01-01T00:00:00Z"); err != nil {
				t.Fatal(err)
			}
		}, true, "retirement_intent_unknown", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			s := archiveTestSession(t, db, "acme", 7)
			tt.prepare(t, db, s)

			got, err := db.GetArchiveOwner(t.Context(), s.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.ArchivePending != tt.wantPending || got.ArchivePendingReason != tt.wantReason || got.ArchivableRows != tt.wantRows {
				t.Fatalf("owner pending=%v reason=%q rows=%d; want %v %q %d", got.ArchivePending, got.ArchivePendingReason, got.ArchivableRows, tt.wantPending, tt.wantReason, tt.wantRows)
			}
			if got.Family != (ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}) {
				t.Fatalf("family = %+v", got.Family)
			}
		})
	}
}

func TestDB_GetArchiveOwner_UnknownSessionReturnsError(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if _, err := db.GetArchiveOwner(t.Context(), 12345, time.Now()); err == nil {
		t.Fatal("want error for unknown session")
	}
}

func TestDB_GetArchiveOwner_CancelledContextReturnsError(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	s := archiveTestSession(t, db, "acme", 7)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.GetArchiveOwner(ctx, s.ID, time.Now()); err == nil {
		t.Fatal("want error on cancelled context")
	}
}

func TestDB_ArchiveSweep_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	zero, err := db.GetArchiveSweep(t.Context())
	if err != nil || !reflect.DeepEqual(zero, ArchiveSweep{}) {
		t.Fatalf("initial = %+v, %v", zero, err)
	}
	want := ArchiveSweep{ID: "s1", StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Boundary: 9, Cursor: 4, Owners: 2,
		Counts: map[string]int{"archived": 1}, Deferred: 1, DeletedAny: true, NextDue: time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)}
	if err := db.SaveArchiveSweep(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetArchiveSweep(t.Context())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	want.Cursor, want.Complete = 9, true
	if err := db.SaveArchiveSweep(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetArchiveSweep(t.Context())
	if got.Cursor != 9 || !got.Complete {
		t.Fatalf("update not persisted: %+v", got)
	}
}

func TestDB_SaveArchiveSweep_RejectsInvalid(t *testing.T) {
	t.Parallel()
	start := time.Now()
	for _, tt := range []struct {
		name string
		s    ArchiveSweep
	}{
		{"no id", ArchiveSweep{StartedAt: start}},
		{"no start", ArchiveSweep{ID: "x"}},
		{"negative cursor", ArchiveSweep{ID: "x", StartedAt: start, Cursor: -1}},
		{"boundary before cursor", ArchiveSweep{ID: "x", StartedAt: start, Cursor: 5, Boundary: 4}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			if err := db.SaveArchiveSweep(t.Context(), tt.s); err == nil {
				t.Fatal("want error")
			}
			got, _ := db.GetArchiveSweep(t.Context())
			if got.ID != "" {
				t.Fatalf("invalid state persisted: %+v", got)
			}
		})
	}
}

func TestDB_DeleteArchivedFamily(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	t.Run("matching digest/deletes family rows only", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		other := archiveTestSession(t, db, "other", 7)
		archiveTestComments(t, db, s.ID, 3)
		archiveTestComments(t, db, other.ID, 2)
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}

		deleted, err := db.DeleteArchivedFamily(t.Context(), f, archiveTestTrailer(t, db, f), now)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(deleted, map[string]int64{"comments": 3}) {
			t.Fatalf("deleted = %v", deleted)
		}
		if n := archiveTestCount(t, db, "comments", s.ID); n != 0 {
			t.Fatalf("family comments left: %d", n)
		}
		if n := archiveTestCount(t, db, "comments", other.ID); n != 2 {
			t.Fatalf("other family comments = %d, want 2", n)
		}
		if _, err := db.GetSession(t.Context(), s.ID); err != nil {
			t.Fatalf("skeleton session removed: %v", err)
		}
	})

	t.Run("digest mismatch/rolls back and deletes nothing", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		archiveTestComments(t, db, s.ID, 3)
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		tr := archiveTestTrailer(t, db, f)
		// Live rows change after the file was made.
		if _, err := db.sql.ExecContext(t.Context(), `UPDATE comments SET body='changed' WHERE session_id=? AND forge_comment_id=1`, s.ID); err != nil {
			t.Fatal(err)
		}

		deleted, err := db.DeleteArchivedFamily(t.Context(), f, tr, now)
		if !errors.Is(err, ErrArchiveDigestMismatch) || deleted != nil {
			t.Fatalf("deleted=%v err=%v; want digest mismatch", deleted, err)
		}
		if n := archiveTestCount(t, db, "comments", s.ID); n != 3 {
			t.Fatalf("comments = %d, want 3 untouched", n)
		}
	})

	t.Run("extra live row/count mismatch", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		archiveTestComments(t, db, s.ID, 2)
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		tr := archiveTestTrailer(t, db, f)
		tr.Counts["comments"] = 1
		if _, err := db.DeleteArchivedFamily(t.Context(), f, tr, now); !errors.Is(err, ErrArchiveDigestMismatch) {
			t.Fatalf("err = %v", err)
		}
		if n := archiveTestCount(t, db, "comments", s.ID); n != 2 {
			t.Fatalf("comments = %d", n)
		}
	})

	t.Run("no longer settled/returns not settled and keeps rows", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		archiveTestComments(t, db, s.ID, 2)
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		tr := archiveTestTrailer(t, db, f)
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO retirement_intents(session_id,sweep_id,kind,resource,expected_sha,base_sha,action,observed_at) VALUES(?,?,?,?,?,?,?,?)`,
			s.ID, "sweep", "worktree", "/x", "a", "b", "remove", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		deleted, err := db.DeleteArchivedFamily(t.Context(), f, tr, now)
		if !errors.Is(err, ErrArchiveNotSettled) || deleted != nil {
			t.Fatalf("deleted=%v err=%v; want not settled", deleted, err)
		}
		if n := archiveTestCount(t, db, "comments", s.ID); n != 2 {
			t.Fatalf("comments = %d", n)
		}
	})
}

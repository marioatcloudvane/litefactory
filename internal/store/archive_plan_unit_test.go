package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// planSeed inserts one row in each of the six protected Plan history tables
// for the parent.
func planSeed(t *testing.T, db *DB, parent, outcome string) {
	t.Helper()
	ctx := t.Context()
	stmts := []string{
		`INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?1,1,'{}')`,
		`INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?1,1,'{}')`,
		`INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES(?1,'k',1)`,
		`INSERT INTO plan_story_candidates(parent,request_id,story_key,candidate,request_json) VALUES(?1,'r','k','c','{}')`,
		`INSERT INTO plan_story_authorities(parent,request_id,authority_json) VALUES(?1,'r','{}')`,
		`INSERT INTO plan_story_owner_edits(parent,story_key,observation_id,candidate,base_revision,observed_revision,observed_title,observed_body,draft_json,stage) VALUES(?1,'k','o','c','b','o','t','b','{}','story')`,
	}
	for _, s := range stmts {
		if _, err := db.sql.ExecContext(ctx, s, parent); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	if outcome != "" {
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_effects(parent,request_id,phase,outcome) VALUES(?,'r','publish',?)`, parent, outcome); err != nil {
			t.Fatalf("seed effect: %v", err)
		}
	}
}

// planDeletes maps each protected table to its delete statement by parent, in
// an order that respects foreign keys.
var planDeletes = []struct{ table, stmt string }{
	{"plan_story_effects", `DELETE FROM plan_story_effects WHERE parent=?`},
	{"plan_story_authorities", `DELETE FROM plan_story_authorities WHERE parent=?`},
	{"plan_workflow_history", `DELETE FROM plan_workflow_history WHERE parent=?`},
	{"plan_story_owner_edits", `DELETE FROM plan_story_owner_edits WHERE parent=?`},
	{"plan_story_candidates", `DELETE FROM plan_story_candidates WHERE parent=?`},
	{"plan_story_drafts", `DELETE FROM plan_story_drafts WHERE parent=?`},
}

func planCount(t *testing.T, db *DB, table, parent string) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table+` WHERE parent=?`, parent).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func grantCount(t *testing.T, db *DB) int64 {
	t.Helper()
	n, err := writePlanArchiveGrants(t.Context(), db.sql, planGrantCount, nil)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPlanArchiveTriggers_DeleteWithoutGrantIsAborted(t *testing.T) {
	t.Parallel()
	for _, d := range planDeletes {
		t.Run(d.table+"/delete aborts and row stays", func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			p1 := archiveTestSession(t, db, "acme", 1).TaskRef
			planSeed(t, db, p1, "applied")
			if _, err := db.sql.ExecContext(t.Context(), d.stmt, p1); err == nil {
				t.Fatal("delete succeeded without grant")
			}
			if n := planCount(t, db, d.table, p1); n == 0 {
				t.Fatalf("%s row was deleted", d.table)
			}
		})
	}
}

func TestPlanArchiveTriggers_GrantIsPerParent(t *testing.T) {
	t.Parallel()
	for _, d := range planDeletes {
		t.Run(d.table+"/grant for A opens A only", func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			A := archiveTestSession(t, db, "acme", 1).TaskRef
			B := archiveTestSession(t, db, "acme", 2).TaskRef
			planSeed(t, db, A, "applied")
			planSeed(t, db, B, "applied")
			if _, err := writePlanArchiveGrants(t.Context(), db.sql, planGrantAdd, []string{A}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.sql.ExecContext(t.Context(), d.stmt, B); err == nil {
				t.Fatal("delete of parent B succeeded under grant for A")
			}
			if n := planCount(t, db, d.table, B); n == 0 {
				t.Fatal("parent B row deleted")
			}
			// A's dependents must go first, so run the ordered deletes up to this table.
			for _, p := range planDeletes {
				if _, err := db.sql.ExecContext(t.Context(), p.stmt, A); err != nil {
					t.Fatalf("delete %s of A under grant: %v", p.table, err)
				}
				if p.table == d.table {
					break
				}
			}
			if n := planCount(t, db, d.table, A); n != 0 {
				t.Fatalf("A rows left: %d", n)
			}
		})
	}
}

func TestPlanArchiveTriggers_UpdateStillRefusedWithGrant(t *testing.T) {
	t.Parallel()
	updates := []struct{ table, stmt string }{
		{"plan_story_candidates", `UPDATE plan_story_candidates SET candidate='x' WHERE parent=?`},
		{"plan_story_effects", `UPDATE plan_story_effects SET detail='x' WHERE parent=?`},
		{"plan_story_drafts", `UPDATE plan_story_drafts SET ordinal=9 WHERE parent=?`},
		{"plan_workflow_history", `UPDATE plan_workflow_history SET state_json='x' WHERE parent=?`},
		{"plan_story_owner_edits", `UPDATE plan_story_owner_edits SET draft_json='x' WHERE parent=?`},
		{"plan_story_authorities", `UPDATE plan_story_authorities SET authority_json='x' WHERE parent=?`},
	}
	for _, u := range updates {
		t.Run(u.table+"/update refused", func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			A := archiveTestSession(t, db, "acme", 1).TaskRef
			planSeed(t, db, A, "applied")
			if _, err := writePlanArchiveGrants(t.Context(), db.sql, planGrantAdd, []string{A}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.sql.ExecContext(t.Context(), u.stmt, A); err == nil {
				t.Fatal("update succeeded under grant")
			}
		})
	}
}

func TestDB_DeleteArchivedFamily_PlanRows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	t.Run("settled plan history/deleted and grant table empty after commit", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		planSeed(t, db, s.TaskRef, "applied")
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}

		deleted, err := db.DeleteArchivedFamily(t.Context(), f, archiveTestTrailer(t, db, f), now)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range planDeletes {
			if n := planCount(t, db, d.table, s.TaskRef); n != 0 {
				t.Errorf("%s rows left: %d", d.table, n)
			}
			if deleted[d.table] != 1 {
				t.Errorf("deleted[%s] = %d, want 1", d.table, deleted[d.table])
			}
		}
		if n := grantCount(t, db); n != 0 {
			t.Fatalf("grant rows after commit: %d", n)
		}
	})

	t.Run("delete does not touch another parent's plan history", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		other := archiveTestSession(t, db, "acme", 8)
		planSeed(t, db, s.TaskRef, "applied")
		planSeed(t, db, other.TaskRef, "applied")
		f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		if _, err := db.DeleteArchivedFamily(t.Context(), f, archiveTestTrailer(t, db, f), now); err != nil {
			t.Fatal(err)
		}
		if n := planCount(t, db, "plan_story_effects", other.TaskRef); n != 1 {
			t.Fatalf("other parent effects = %d, want 1", n)
		}
	})

	for _, outcome := range []string{"", "pending", "uncertain"} {
		name := outcome
		if name == "" {
			name = "no effect recorded"
		}
		t.Run(name+"/keeps owner and plan rows", func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			s := archiveTestSession(t, db, "acme", 7)
			planSeed(t, db, s.TaskRef, outcome)
			f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
			tr := archiveTestTrailer(t, db, f)

			o, err := db.GetArchiveOwner(t.Context(), s.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			if !o.ArchivePending || o.ArchivePendingReason != "plan_effect_unsettled" {
				t.Fatalf("pending=%v reason=%q", o.ArchivePending, o.ArchivePendingReason)
			}
			deleted, err := db.DeleteArchivedFamily(t.Context(), f, tr, now)
			if !errors.Is(err, ErrArchiveNotSettled) || deleted != nil {
				t.Fatalf("deleted=%v err=%v; want not settled", deleted, err)
			}
			if n := planCount(t, db, "plan_story_drafts", s.TaskRef); n != 1 {
				t.Fatalf("drafts = %d", n)
			}
			if n := grantCount(t, db); n != 0 {
				t.Fatalf("grants = %d", n)
			}
		})
	}

	t.Run("latest effect applied after uncertain/is settled", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		s := archiveTestSession(t, db, "acme", 7)
		planSeed(t, db, s.TaskRef, "uncertain")
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO plan_story_effects(parent,request_id,phase,outcome) VALUES(?,'r','publish','applied')`, s.TaskRef); err != nil {
			t.Fatal(err)
		}
		o, err := db.GetArchiveOwner(t.Context(), s.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if o.ArchivePendingReason == "plan_effect_unsettled" {
			t.Fatalf("reason %q with latest effect applied", o.ArchivePendingReason)
		}
	})
}

func TestWritePlanArchiveGrants_GrantsEmptyAfterRollback(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	boom := errors.New("boom")
	err := db.withTx(t.Context(), func(tx *sql.Tx) error {
		if _, err := writePlanArchiveGrants(t.Context(), tx, planGrantAdd, []string{"A", "B"}); err != nil {
			return err
		}
		n, err := writePlanArchiveGrants(t.Context(), tx, planGrantCount, nil)
		if err != nil || n != 2 {
			t.Errorf("in-tx count = %d err=%v", n, err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if n := grantCount(t, db); n != 0 {
		t.Fatalf("grant rows after rollback: %d", n)
	}
}

func TestWritePlanArchiveGrants_Operations(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	if n, err := writePlanArchiveGrants(ctx, db.sql, planGrantAdd, []string{"A", "A", "B"}); err != nil || n != 2 {
		t.Fatalf("add = %d, %v; want 2 (duplicate ignored)", n, err)
	}
	if n, err := writePlanArchiveGrants(ctx, db.sql, planGrantRemove, []string{"A"}); err != nil || n != 1 {
		t.Fatalf("remove = %d, %v", n, err)
	}
	if n := grantCount(t, db); n != 1 {
		t.Fatalf("count = %d", n)
	}
	if n, err := writePlanArchiveGrants(ctx, db.sql, planGrantPurge, nil); err != nil || n != 1 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	if _, err := writePlanArchiveGrants(ctx, db.sql, planGrantOp(99), nil); err == nil {
		t.Fatal("unknown operation accepted")
	}
}

func TestOpen_ClearsLeftoverPlanArchiveGrants(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "litefactory.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := writePlanArchiveGrants(t.Context(), db.sql, planGrantAdd, []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	if n := grantCount(t, db); n != 2 {
		t.Fatalf("planted = %d", n)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if n := grantCount(t, db2); n != 0 {
		t.Fatalf("grant rows after reopen: %d", n)
	}
}

func TestOpen_UnmigratedDatabaseToleratesMissingGrantTable(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open unmigrated: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

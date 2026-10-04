package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func archiveUpgradeDB(t *testing.T, version int) (*DB, string, []migration) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var selected []migration
	for _, m := range migrations {
		if m.version <= version {
			selected = append(selected, m)
		}
	}
	if err = db.migrateAll(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	return db, file, migrations
}

func archiveLogicalRows(t *testing.T, db *DB, query string) string {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var data [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err = rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		data = append(data, vals)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(struct {
		Columns []string
		Rows    [][]any
	}{cols, data})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBugArchivePreservesHistoryAndRefusesAllWrites(t *testing.T) {
	db, path, migrations := archiveUpgradeDB(t, 95)
	session := testSession(t, db, 984)
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/legacy.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "legacy", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "legacy", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	seed := []string{
		fmt.Sprintf("INSERT INTO bug_tasks(session_id,repository,issue_id,issue_number,owner_id,revision) VALUES(%d,'acme/api',984,984,42,7)", session.ID),
		fmt.Sprintf("INSERT INTO bug_analyses(session_id,invocation,input_digest,ordinal,graph_attempt_id,state,content,evidence,reason) VALUES(%d,'old-analysis','%s',1,%d,'failed',X'00FF','historic evidence','original failure')", session.ID, strings.Repeat("a", 64), attempt.ID),
		fmt.Sprintf("INSERT INTO bug_repairs(session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,state,completion_evidence) VALUES(%d,'old-repair','digest',4,1,'digest','review','initial','retained','commit','tree',%d,'uncertain',X'00FE')", session.ID, attempt.ID),
		fmt.Sprintf("INSERT INTO bug_checks(session_id,operation,input_digest,binding,recipe,command_count,commands,reason) VALUES(%d,'old-check','digest',X'00FD',X'00FC',1,'[{\"historic\":true}]','lost acknowledgment')", session.ID),
		fmt.Sprintf("INSERT INTO bug_delivery(session_id,pr_operation,pr_input_digest,pr_branch,pr_creator_id,pr_marker,merge_outcome,merge_reason) VALUES(%d,'old-pr','digest','lf/old',5,'marker','pending','uncertain remote effect')", session.ID),
		fmt.Sprintf("INSERT INTO bug_events(session_id,kind,reference,content,recorded_at) VALUES(%d,'historic','old',X'00FB','2026-01-01T00:00:00Z')", session.ID),
	}
	for _, q := range seed {
		if _, err = db.sql.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"bug_tasks", "bug_analyses", "bug_repairs", "bug_checks", "bug_delivery", "bug_events"}
	before := make(map[string]string)
	for _, table := range tables {
		before[table] = archiveLogicalRows(t, db, "SELECT * FROM "+table)
		before[table+"schema"] = archiveLogicalRows(t, db, "SELECT sql FROM sqlite_master WHERE type='table' AND name='"+table+"'")
		before[table+"fk"] = archiveLogicalRows(t, db, "PRAGMA foreign_key_list("+table+")")
	}
	versions := archiveLogicalRows(t, db, "SELECT * FROM schema_migrations ORDER BY version")
	if err = db.migrateAll(t.Context(), migrations); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if got := archiveLogicalRows(t, db, "SELECT * FROM "+table); got != before[table] {
			t.Fatalf("%s history changed", table)
		}
		if got := archiveLogicalRows(t, db, "SELECT sql FROM sqlite_master WHERE type='table' AND name='"+table+"'"); got != before[table+"schema"] {
			t.Fatalf("%s schema changed", table)
		}
		if got := archiveLogicalRows(t, db, "PRAGMA foreign_key_list("+table+")"); got != before[table+"fk"] {
			t.Fatalf("%s foreign keys changed", table)
		}
		for _, q := range []string{"UPDATE " + table + " SET session_id=session_id", "DELETE FROM " + table, "INSERT INTO " + table + " SELECT * FROM " + table} {
			if _, err = db.sql.ExecContext(t.Context(), q); err == nil || !strings.Contains(err.Error(), "read-only") {
				t.Fatalf("%s allowed archive write: %v", q, err)
			}
		}
	}
	if got := archiveLogicalRows(t, db, "SELECT * FROM schema_migrations WHERE version<96 ORDER BY version"); got != versions {
		t.Fatal("applied history changed")
	}
	// Parent deletion is refused even if the historical task appears terminal.
	if _, err = db.sql.ExecContext(t.Context(), "DELETE FROM workflow_admissions WHERE session_id=?", session.ID); err == nil {
		t.Fatal("ancestor deletion removed retained history")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if got := archiveLogicalRows(t, db, "SELECT * FROM "+table); got != before[table] {
			t.Fatalf("%s history lost on reopen", table)
		}
	}
}

func TestBugArchivePre0079PopulatedPreservationRefusal(t *testing.T) {
	db, _, migrations := archiveUpgradeDB(t, 78)
	session := testSession(t, db, 985)
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), "INSERT INTO bug_records VALUES(?,1,'acme/api',985,985,42,'bug',1)", session.ID); err != nil {
		t.Fatal(err)
	}
	before := archiveLogicalRows(t, db, "SELECT * FROM bug_records")
	err := db.migrateAll(t.Context(), migrations)
	if err == nil || !strings.Contains(err.Error(), "separately reviewed preservation upgrade") {
		t.Fatalf("unsafe upgrade: %v", err)
	}
	if got := archiveLogicalRows(t, db, "SELECT * FROM bug_records"); got != before {
		t.Fatal("old records mutated")
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM schema_migrations WHERE version>=79").Scan(&count); err != nil || count != 0 {
		t.Fatalf("destructive migration recorded: %d %v", count, err)
	}
}

func TestBugArchiveMigrationFailureRollsBack(t *testing.T) {
	db, _, migrations := archiveUpgradeDB(t, 95)
	var archive migration
	for _, m := range migrations {
		if m.version == 96 {
			archive = m
		}
	}
	// Fail late, after every trigger would otherwise have been created.
	archive.sql += "\nCREATE TABLE impossible (x INVALID CONSTRAINT);\nTHIS IS INVALID SQL;"
	if err := db.applyMigration(t.Context(), archive); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'archive_bug_%'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial archive: %d %v", count, err)
	}
	var recorded int
	if err := db.sql.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM schema_migrations WHERE version=96").Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("failed migration recorded: %d %v", recorded, err)
	}
	if err := db.migrateAll(t.Context(), migrations); err != nil {
		t.Fatal(err)
	}
}

func TestBugArchiveFreshAndEmptyOlderUpgrades(t *testing.T) {
	for _, version := range []int{0, 78, 95} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db, _, migrations := archiveUpgradeDB(t, version)
			if err := db.migrateAll(t.Context(), migrations); err != nil {
				t.Fatal(err)
			}
			first, err := db.appliedVersions(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			second, err := db.appliedVersions(t.Context())
			if err != nil || !reflect.DeepEqual(first, second) {
				t.Fatal("repeat migration changed versions", err)
			}
			var count int
			if err = db.sql.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'archive_bug_%'").Scan(&count); err != nil || count != 18 {
				t.Fatalf("archive trigger count=%d %v", count, err)
			}
		})
	}
}

func TestBugArchiveUnsupportedLegacyOrphanRemainsUntouched(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 986)
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("b", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "legacy", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "legacy-bug", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude, TriggerCommentID: 500, LogPath: func(int) string { return "/retained/legacy.log" }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "legacy", RunID: run.ID, StartedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.AcquireSessionLock(t.Context(), session.ID, "old-owner"); err != nil || !ok {
		t.Fatal("lock", ok, err)
	}
	before := map[string]string{}
	for _, table := range []string{"sessions", "runs", "graph_instances", "graph_node_attempts"} {
		before[table] = archiveLogicalRows(t, db, "SELECT * FROM "+table)
	}
	result, err := db.ReconcileOrphans(t.Context(), "replacement-owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AbandonedRuns) != 0 || len(result.ReclaimedLocks) != 0 {
		t.Fatalf("legacy orphan reclassified: %+v", result)
	}
	for table, want := range before {
		if got := archiveLogicalRows(t, db, "SELECT * FROM "+table); got != want {
			t.Fatalf("legacy %s mutated", table)
		}
	}
}

func TestBugArchiveCleanupKeepsTerminalHistoricalFamily(t *testing.T) {
	db, _, migrations := archiveUpgradeDB(t, 95)
	session := testSession(t, db, 987)
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), "INSERT INTO bug_tasks(session_id,repository,issue_id,issue_number,owner_id,revision,disposition,cancelled) VALUES(?,'acme/api',987,987,42,1,'completed',1)", session.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateAll(t.Context(), migrations); err != nil {
		t.Fatal(err)
	}
	reason, err := archivePendingReason(t.Context(), db.sql, ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef}, graphTestTime())
	if err != nil || reason != "bug_history_retained" {
		t.Fatalf("terminal legacy family offered to cleanup: %q %v", reason, err)
	}
}

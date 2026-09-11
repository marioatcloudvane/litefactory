package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// applyMigrationsThrough migrates db up to and including version, using the
// same unexported machinery [DB.Migrate] uses, so a test can stand a database
// at an intermediate schema version and inspect it before the rest of the
// sequence runs.
// migrationVersionBefore is the version immediately preceding the migration
// whose name is given. Tests that need a database as it stood before one
// specific migration ask for it by name: a hardcoded number silently stops
// meaning "just before this migration" the moment anything is renumbered, and
// then fails somewhere unrelated.
func migrationVersionBefore(t *testing.T, name string) int {
	t.Helper()

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	for _, m := range migrations {
		if m.name == name {
			return m.version - 1
		}
	}
	t.Fatalf("no migration named %q", name)
	return 0
}

func applyMigrationsThrough(t *testing.T, db *DB, version int) {
	t.Helper()

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := db.ensureMigrationTable(t.Context()); err != nil {
		t.Fatalf("ensure migration table: %v", err)
	}
	for _, m := range migrations {
		if m.version > version {
			break
		}
		if err := db.applyMigration(t.Context(), m); err != nil {
			t.Fatalf("apply migration %d: %v", m.version, err)
		}
	}
}

// columnInfo is one row of `PRAGMA table_info`, the shape a schema-constraint
// snapshot is built from.
type columnInfo struct {
	name    string
	sqlType string
	notNull bool
	pk      int
}

// tableColumns snapshots every column of table, in schema order.
func tableColumns(t *testing.T, db *DB, table string) []columnInfo {
	t.Helper()

	// #nosec G202 -- table is a fixed literal from within this test file,
	// never external input; PRAGMA does not accept a bound parameter for the
	// table name.
	rows, err := db.sql.QueryContext(t.Context(), `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()

	var out []columnInfo
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		out = append(out, columnInfo{name: name, sqlType: colType, notNull: notNull != 0, pk: pk})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read table_info(%s): %v", table, err)
	}
	return out
}

// indexNames lists every index SQLite has recorded on table, so a migration
// that renames a column but must not touch the table's constraint set can be
// checked structurally rather than by inspection (AC-4.3).
func indexNames(t *testing.T, db *DB, table string) []string {
	t.Helper()

	// #nosec G202 -- table is a fixed literal from within this test file.
	rows, err := db.sql.QueryContext(t.Context(), `PRAGMA index_list(`+table+`)`)
	if err != nil {
		t.Fatalf("index_list(%s): %v", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index_list(%s): %v", table, err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read index_list(%s): %v", table, err)
	}
	return out
}

// TestMigration0003RenamesSessionRefWithoutChangingItsConstraints is AC-4.3:
// a schema-constraint snapshot taken before and after the RENAME COLUMN, on
// the column under its two names, must agree on everything but the name
// itself — and the table's index set (which is what a UNIQUE constraint would
// show up as) must be byte-for-byte the same, because nothing in this
// migration touches it.
func TestMigration0003RenamesSessionRefWithoutChangingItsConstraints(t *testing.T) {
	t.Parallel()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	applyMigrationsThrough(t, db, 1)
	before := tableColumns(t, db, "segments")
	beforeIndexes := indexNames(t, db, "segments")

	var beforeCol *columnInfo
	for i := range before {
		if before[i].name == "claude_session_id" {
			beforeCol = &before[i]
		}
	}
	if beforeCol == nil {
		t.Fatalf("segments has no claude_session_id column before migration 0003: %+v", before)
	}

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate the rest of the sequence: %v", err)
	}

	after := tableColumns(t, db, "segments")
	afterIndexes := indexNames(t, db, "segments")

	var afterCol *columnInfo
	for i := range after {
		if after[i].name == "session_ref" {
			afterCol = &after[i]
		}
	}
	if afterCol == nil {
		t.Fatalf("segments has no session_ref column after migration 0003: %+v", after)
	}
	if _, stillThere := findColumn(after, "claude_session_id"); stillThere {
		t.Error("segments still has a claude_session_id column after the rename")
	}

	if afterCol.sqlType != beforeCol.sqlType || afterCol.notNull != beforeCol.notNull || afterCol.pk != beforeCol.pk {
		t.Fatalf("session_ref's constraints are %+v, want claude_session_id's pre-rename constraints %+v",
			*afterCol, *beforeCol)
	}
	if !afterCol.notNull {
		t.Error("session_ref lost its NOT NULL constraint across the rename")
	}

	if len(after) != len(before)+2 {
		t.Fatalf("segments has %d columns after migration, want %d (the pre-migration %d plus agent and resume_token)",
			len(after), len(before)+2, len(before))
	}

	if !equalStrings(beforeIndexes, afterIndexes) {
		t.Fatalf("segments' index set changed across the rename: before %v, after %v", beforeIndexes, afterIndexes)
	}
}

// TestMigration0007AddsRunSyncColumnsWithSafeDefaults is AC-9.1: the new
// migration applies cleanly on top of the current latest (0006), and a run
// written before it existed reads back with the new columns' defaults
// rather than an error.
func TestMigration0010AddsRunSyncColumnsWithSafeDefaults(t *testing.T) {
	t.Parallel()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	// Everything before 0010_run_sync, so the run seeded below predates the
	// sync columns exactly as a real pre-migration row would. testRepo needs
	// 0007_repo_external_team's column, so stopping at 6 is not an option.
	applyMigrationsThrough(t, db, 9)

	repo := testRepo(t, db, "acme", "api")
	session, err := db.GetOrCreateSession(t.Context(), repo, 123)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "claude-session-id", AgentClaude)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}
	res, err := db.sql.ExecContext(t.Context(), `
INSERT INTO runs (
    session_id, segment_id, ordinal, mode, agent, trigger_comment_id, status,
    started_at, log_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, segment.ID, 1, ModeSpike, AgentClaude, 0, string(RunRunning), FormatTime(nowUTC()), "/logs/0001.jsonl")
	if err != nil {
		t.Fatalf("seed pre-migration run: %v", err)
	}
	runID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("run id: %v", err)
	}

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate the rest of the sequence: %v", err)
	}

	cols := tableColumns(t, db, "runs")
	for _, want := range []struct {
		name    string
		notNull bool
	}{
		{"sync_outcome", true},
		{"sync_behind", true},
		{"sync_ahead", true},
		{"sync_commit_sha", false},
	} {
		col, ok := findColumn(cols, want.name)
		if !ok {
			t.Fatalf("runs has no %s column after migration 0010: %+v", want.name, cols)
		}
		if col.notNull != want.notNull {
			t.Errorf("%s.notNull = %v, want %v", want.name, col.notNull, want.notNull)
		}
	}

	run, err := db.GetRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("read migrated run: %v", err)
	}
	if run.SyncOutcome != "" || run.SyncBehind != 0 || run.SyncAhead != 0 || run.SyncCommitSHA != "" {
		t.Errorf("pre-migration run got sync fields %+v, want all zero values", run)
	}
}

func findColumn(cols []columnInfo, name string) (columnInfo, bool) {
	for _, c := range cols {
		if c.name == name {
			return c, true
		}
	}
	return columnInfo{}, false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMigration0003BackfillsAgentAndLeavesResumeTokenNull is AC-4.1, AC-4.2 and
// AC-4.4: a row written before this migration existed keeps its session ref,
// is backfilled agent = 'claude' on both segments and runs — via the column's
// DEFAULT, never a separate UPDATE — and comes back with resume_token NULL.
func TestMigration0003BackfillsAgentAndLeavesResumeTokenNull(t *testing.T) {
	t.Parallel()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	applyMigrationsThrough(t, db, 2)

	// Seed the historical schema directly; current UpsertRepo legitimately
	// requires columns introduced after this migration under test.
	repo := Repo{ID: 1, Path: filepath.Join(t.TempDir(), "api"), Forge: "github", Owner: "acme", Name: "api"}
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO repos (id,path,forge,owner,name,remote_url,base_branch,feature_prefix,worktree_root,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, repo.ID, repo.Path, repo.Forge, repo.Owner, repo.Name, "https://github.com/acme/api.git", "main", "lf", repo.Path+"-worktrees", FormatTime(nowUTC()), FormatTime(nowUTC()))
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 123)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const seededSessionRef = "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e"
	res, err := db.sql.ExecContext(t.Context(), `
INSERT INTO segments (session_id, ordinal, claude_session_id, created_at)
VALUES (?, ?, ?, ?)`,
		session.ID, FirstSegmentOrdinal, seededSessionRef, FormatTime(nowUTC()))
	if err != nil {
		t.Fatalf("seed pre-migration segment: %v", err)
	}
	segmentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("segment id: %v", err)
	}

	res, err = db.sql.ExecContext(t.Context(), `
INSERT INTO runs (
    session_id, segment_id, ordinal, mode, trigger_comment_id, status,
    started_at, log_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, segmentID, 1, ModeSpike, 0, string(RunRunning), FormatTime(nowUTC()), "/logs/0001.jsonl")
	if err != nil {
		t.Fatalf("seed pre-migration run: %v", err)
	}
	runID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("run id: %v", err)
	}

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate the rest of the sequence: %v", err)
	}

	segment, err := db.GetSegment(t.Context(), segmentID)
	if err != nil {
		t.Fatalf("read migrated segment: %v", err)
	}
	if segment.SessionRef != seededSessionRef {
		t.Errorf("session_ref = %q, want the pre-migration claude_session_id %q", segment.SessionRef, seededSessionRef)
	}
	if segment.Agent != AgentClaude {
		t.Errorf("segment agent = %q, want %q (backfilled by the column DEFAULT)", segment.Agent, AgentClaude)
	}
	if segment.ResumeToken != "" {
		t.Errorf("resume_token = %q, want empty (NULL) for a backfilled row", segment.ResumeToken)
	}

	run, err := db.GetRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("read migrated run: %v", err)
	}
	if run.Agent != AgentClaude {
		t.Errorf("run agent = %q, want %q (backfilled by the column DEFAULT)", run.Agent, AgentClaude)
	}

	// AC-4.4's other half: a post-migration Claude resume still resumes.
	// GetOrCreateSegment's stored-row-wins semantics is what a resuming turn
	// relies on — it must hand back the same session_ref every earlier run of
	// this task used, even when called with different values, because that id
	// is the one the tool's own transcript is keyed on.
	resumed, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "a-freshly-derived-id", AgentClaude)
	if err != nil {
		t.Fatalf("get or create segment post-migration: %v", err)
	}
	if resumed.ID != segmentID || resumed.SessionRef != seededSessionRef {
		t.Fatalf("post-migration resume would target %+v, want the pre-migration segment %d with session_ref %q",
			resumed, segmentID, seededSessionRef)
	}
}

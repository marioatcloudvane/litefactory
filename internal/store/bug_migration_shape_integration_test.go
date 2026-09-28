package store

// Integration coverage for story-001 (issue #425): running the full embedded
// migration set against a real sqlite database, from empty through the
// latest migration, must leave exactly the six new bug_ tables in place and
// none of the 33 retired ones — and a fresh database migrated straight to
// head must reach the same shape as one migrated to just before the
// consolidation and then the rest of the way.

import (
	"path/filepath"
	"sort"
	"testing"
)

// bugTables are the six tables 0079/0080 replace the 33 retired ones with.
var bugTables = []string{"bug_tasks", "bug_analyses", "bug_repairs", "bug_checks", "bug_delivery", "bug_events"}

// retiredBugTables are every bug_* table 0079 drops. Listed explicitly (not
// derived from the migration file) so this test fails loudly if a future
// migration reintroduces one of these exact names for something else.
var retiredBugTables = []string{
	"bug_analysis_events", "bug_analysis_approvals", "bug_analysis_review_changes",
	"bug_analysis_reviews", "bug_analysis_answers", "bug_analysis_revisions",
	"bug_analysis_passes", "bug_execution_sections", "bug_test_resolutions",
	"bug_test_events", "bug_test_calls", "bug_check_events", "bug_check_commands",
	"bug_check_phases", "bug_repair_scope_observations", "bug_repair_completions",
	"bug_closure_retries", "bug_closure_observations", "bug_closures",
	"bug_merge_retries", "bug_merge_observations", "bug_merges",
	"bug_delivery_replies", "bug_delivery_reviews", "bug_pr_observations",
	"bug_pr_intents", "bug_parent_continuations", "bug_lifecycle_events",
	"bug_lifecycles", "bug_work_guards", "bug_execution", "bug_records",
}

func TestBugSchemaConsolidation_FreshMigrationHasExactlySixBugTables(t *testing.T) {
	t.Parallel()
	db := newTestDB(t) // migrates the complete embedded set, empty to head

	for _, table := range bugTables {
		if !bugTableExists(t, db, table) {
			t.Errorf("expected consolidated table %q to exist after a fresh migration", table)
		}
	}
	for _, table := range retiredBugTables {
		if bugTableExists(t, db, table) {
			t.Errorf("retired table %q still exists after a fresh migration to head", table)
		}
	}

	got := bugTablesLike(t, db)
	sort.Strings(got)
	want := append([]string{}, bugTables...)
	sort.Strings(want)
	if !equalStrings(got, want) {
		t.Fatalf("bug_* tables after a fresh migration = %v, want exactly %v", got, want)
	}
}

// TestBugSchemaConsolidation_UpgradedMatchesFresh is story-001's other half:
// a database migrated to just before the consolidation (so it still carries
// the 33 retired tables and whatever data an in-flight release check left
// behind) and then the rest of the way to head must land on exactly the same
// sqlite_master shape as a database that went straight from empty to head.
func TestBugSchemaConsolidation_UpgradedMatchesFresh(t *testing.T) {
	t.Parallel()

	fresh := newTestDB(t)
	freshShape := sqliteMasterShape(t, fresh)

	upgraded, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := upgraded.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	applyMigrationsThrough(t, upgraded, migrationVersionBefore(t, "bug_store_consolidation"))

	// The pre-consolidation schema really does still carry the retired tables:
	// otherwise this test would trivially pass by comparing two already-fresh
	// schemas.
	for _, table := range retiredBugTables {
		if !bugTableExists(t, upgraded, table) {
			t.Fatalf("pre-consolidation database is missing retired table %q; fixture no longer stands at the expected migration", table)
		}
	}

	if err := upgraded.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate the rest of the sequence: %v", err)
	}

	upgradedShape := sqliteMasterShape(t, upgraded)
	if !equalStrings(freshShape, upgradedShape) {
		t.Fatalf("upgraded schema shape differs from a fresh migration:\nfresh:    %v\nupgraded: %v",
			diffStrings(freshShape, upgradedShape), diffStrings(upgradedShape, freshShape))
	}
}

func bugTableExists(t *testing.T, db *DB, table string) bool {
	t.Helper()
	var name string
	err := db.sql.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	return err == nil
}

func bugTablesLike(t *testing.T, db *DB) []string {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'bug\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatalf("list bug tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list bug tables: %v", err)
	}
	return out
}

// sqliteMasterShape snapshots every table, index and trigger's defining SQL,
// sorted, so two databases can be compared structurally regardless of the
// order sqlite_master happens to return rows in.
func sqliteMasterShape(t *testing.T, db *DB) []string {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `SELECT type,name,COALESCE(sql,'') FROM sqlite_master WHERE type IN ('table','index','trigger') AND name NOT IN ('schema_migrations','sqlite_sequence') ORDER BY type,name`)
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, name, sql string
		if err := rows.Scan(&kind, &name, &sql); err != nil {
			t.Fatalf("scan sqlite_master: %v", err)
		}
		out = append(out, kind+":"+name+":"+sql)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	sort.Strings(out)
	return out
}

// diffStrings returns the elements of a not present in b, for a readable
// failure message on a shape mismatch.
func diffStrings(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	var out []string
	for _, v := range a {
		if !inB[v] {
			out = append(out, v)
		}
	}
	return out
}

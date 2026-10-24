package store

import (
	"path/filepath"
	"testing"
)

// A book retained before standards were structured still migrates: its text
// becomes rule and description, its first category stays, and the retained
// exception policy keeps its reference to the altered table.
func TestStandardsStructuredMigrationKeepsRetainedRows(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "standards_structured"))
	for _, statement := range []string{
		`INSERT INTO standards_revisions(id,parent_revision,schema_version,digest,created_at) VALUES(1,NULL,1,'d','2026-10-01T00:00:00Z')`,
		`INSERT INTO standards_categories(revision_id,id,name,description) VALUES(1,'b','B',''),(1,'a','A','')`,
		`INSERT INTO standards_standards(revision_id,id,title,body) VALUES(1,'s','Title','Body')`,
		`INSERT INTO standards_memberships(revision_id,standard_id,category_id) VALUES(1,'s','b'),(1,'s','a')`,
		`INSERT INTO standards_exception_policies(revision_id,standard_id,policy_json) VALUES(1,'s','{"version":1,"mode":"deny"}')`,
	} {
		if _, err := db.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var category, rule, description, check string
	if err := db.sql.QueryRowContext(ctx, `SELECT category_id,rule,description,check_kind FROM standards_standards WHERE revision_id=1 AND id='s'`).Scan(&category, &rule, &description, &check); err != nil {
		t.Fatal(err)
	}
	if category != "a" || rule != "Title" || description != "Body" || check != "" {
		t.Fatalf("migrated row: %q %q %q %q", category, rule, description, check)
	}
	var policies int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM standards_exception_policies`).Scan(&policies); err != nil || policies != 1 {
		t.Fatalf("policy lost: %d %v", policies, err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE standards_standards SET rule='x'`); err == nil {
		t.Fatal("migrated standards are mutable")
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO standards_standards(revision_id,id,category_id,rule,description,evidence,scope,check_kind) VALUES(1,'t','missing','r','d','e','s','manual')`); err == nil {
		t.Fatal("standard with a missing category accepted")
	}
}

package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The template fast path must produce exactly what the real sequence does, and
// leave the connection as Open configured it.
func TestMigrateFromTemplateMatchesRealMigrations(t *testing.T) {
	ctx := t.Context()

	migrated := openBare(t)
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrated.migrateAll(ctx, migrations); err != nil {
		t.Fatal(err)
	}

	restored := openBare(t)
	ok, err := restored.migrateEmptyFromTemplate(ctx, migrations)
	if err != nil || !ok {
		t.Fatalf("migrateEmptyFromTemplate = %v, %v; want true, nil", ok, err)
	}

	if got, want := schemaOf(t, restored), schemaOf(t, migrated); got != want {
		t.Fatalf("restored schema differs from migrated schema\nrestored:\n%s\nmigrated:\n%s", got, want)
	}
	if got, want := versionsOf(t, restored), versionsOf(t, migrated); got != want {
		t.Fatalf("schema_migrations = %s, want %s", got, want)
	}
	var mode string
	if err := restored.sql.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
	if err := restored.verifyForeignKeys(ctx); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err := restored.sql.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q, %v", integrity, err)
	}
	// The restored database stays writable and migrates as a no-op.
	if err := restored.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

// A database that already holds any schema is migrated for real, never
// overwritten from the template.
func TestMigrateFromTemplateLeavesNonEmptyDatabases(t *testing.T) {
	ctx := t.Context()
	db := openBare(t)
	if _, err := db.sql.ExecContext(ctx, `CREATE TABLE planted (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	ok, err := db.migrateEmptyFromTemplate(ctx, migrations)
	if err != nil || ok {
		t.Fatalf("migrateEmptyFromTemplate = %v, %v; want false, nil", ok, err)
	}
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='planted'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("planted table = %d, %v; want kept", n, err)
	}
}

func openBare(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "lf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func schemaOf(t *testing.T, db *DB) string {
	t.Helper()
	return joinRows(t, db, `SELECT type||' '||name||' '||COALESCE(sql,'') FROM sqlite_master ORDER BY type, name`)
}

func versionsOf(t *testing.T, db *DB) string {
	t.Helper()
	return joinRows(t, db, `SELECT version||' '||name FROM schema_migrations ORDER BY version`)
}

func joinRows(t *testing.T, db *DB, query string) string {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, "\n")
}

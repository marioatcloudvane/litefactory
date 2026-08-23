package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// migrationFS holds the schema. The files are embedded so that a migration is
// part of the binary rather than something that has to be shipped alongside it
// (D-1: one static binary).
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationDir is the directory inside migrationFS holding the .sql files.
const migrationDir = "migrations"

// migration is one embedded .sql file. Version comes from the NNNN_ filename
// prefix and orders the sequence.
type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies every migration the database has not seen yet, in version
// order, each in its own transaction. SQLite's DDL is transactional, so a
// migration that fails part way leaves no partial schema behind — which is why
// litefactory needs no migration library (a rejected dependency).
//
// Migrate is idempotent: running it against an up-to-date database does
// nothing and returns nil, so every entry point may call it.
func (db *DB) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if err := db.ensureMigrationTable(ctx); err != nil {
		return err
	}
	applied, err := db.appliedVersions(ctx)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := db.applyMigration(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the highest migration version applied, or 0 for a
// database that has never been migrated.
func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	if err := db.ensureMigrationTable(ctx); err != nil {
		return 0, err
	}
	var version sql.NullInt64
	err := db.sql.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return int(scanID(version)), nil
}

func (db *DB) ensureMigrationTable(ctx context.Context) error {
	const stmt = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at TEXT NOT NULL
)`
	if _, err := db.sql.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}
	return nil
}

func (db *DB) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("store: read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("store: read applied migrations: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read applied migrations: %w", err)
	}
	return applied, nil
}

func (db *DB) applyMigration(ctx context.Context, m migration) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, FormatTime(nowUTC()))
		if err != nil {
			return fmt.Errorf("record: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: migration %04d_%s: %w", m.version, m.name, err)
	}
	return nil
}

// loadMigrations reads and validates the embedded migration set. A filename
// that does not start with a numeric NNNN_ prefix, or a duplicated version, is
// a programming error in this package and fails loudly rather than being
// skipped.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, migrationDir)
	if err != nil {
		return nil, fmt.Errorf("store: read embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, name, err := parseMigrationName(entry.Name())
		if err != nil {
			return nil, err
		}
		if previous, dup := seen[version]; dup {
			return nil, fmt.Errorf("store: migrations %s and %s share version %d", previous, entry.Name(), version)
		}
		seen[version] = entry.Name()

		body, err := migrationFS.ReadFile(path.Join(migrationDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("store: read migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(body)})
	}
	if len(migrations) == 0 {
		return nil, errors.New("store: no embedded migrations found")
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// parseMigrationName splits "0001_init.sql" into version 1 and name "init".
func parseMigrationName(filename string) (int, string, error) {
	base := strings.TrimSuffix(filename, ".sql")
	prefix, name, found := strings.Cut(base, "_")
	if !found || prefix == "" || name == "" {
		return 0, "", fmt.Errorf("store: migration %q does not match NNNN_name.sql", filename)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, "", fmt.Errorf("store: migration %q does not start with a positive NNNN version", filename)
	}
	return version, name, nil
}

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

// migration is one embedded .sql file. Version comes from the numeric filename
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
	if migrateFromTemplate {
		if restored, err := db.migrateEmptyFromTemplate(ctx, migrations); restored || err != nil {
			return err
		}
	}
	return db.migrateAll(ctx, migrations)
}

// migrateAll applies each migration in migrations that db has not recorded.
func (db *DB) migrateAll(ctx context.Context, migrations []migration) error {
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
	if strings.HasPrefix(m.sql, "-- litefactory:rebuild-sessions\n") {
		return db.applySessionRebuild(ctx, m)
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if m.version == 79 {
			if err := preservePreArchiveBugHistory(ctx, tx, m.sql); err != nil {
				return err
			}
		}
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
	return loadMigrationsFS(migrationFS, migrationDir)
}

// ErrDuplicateMigrationVersion identifies migration filenames sharing a version.
var ErrDuplicateMigrationVersion = errors.New("duplicate migration version")

// ValidateMigrationFS checks a migration directory using the runtime loader's
// filename, duplicate-version and readability checks. Missing directories are
// errors; callers decide whether their repository requires migrations.
func ValidateMigrationFS(fsys fs.FS, dir string) error {
	_, err := loadMigrationsFS(fsys, dir)
	return err
}

func loadMigrationsFS(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("store: read migrations: %w", err)
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
			return nil, fmt.Errorf("store: migrations %s and %s share version %d: %w", previous, entry.Name(), version, ErrDuplicateMigrationVersion)
		}
		seen[version] = entry.Name()

		body, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("store: read migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(body)})
	}
	if len(migrations) == 0 {
		return nil, errors.New("store: no migrations found")
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

// preservePreArchiveBugHistory refuses the historical destructive consolidation
// when any table it would drop still contains evidence. The historical SQL is
// unchanged; empty installations retain the normal migration chain.
func preservePreArchiveBugHistory(ctx context.Context, tx *sql.Tx, body string) error {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "DROP" || fields[1] != "TABLE" {
			continue
		}
		table := strings.TrimSuffix(fields[2], ";")
		if !strings.HasPrefix(table, "bug_") {
			return errors.New("store: unexpected destructive Bug migration table")
		}
		for _, r := range table {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
				return errors.New("store: invalid historical Bug table name")
			}
		}
		var populated int
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" LIMIT 1)").Scan(&populated); err != nil {
			return fmt.Errorf("inspect pre-0079 Bug history %s: %w", table, err)
		}
		if populated != 0 {
			return fmt.Errorf("preservation refusal: populated %s would be destroyed by migration 0079; a separately reviewed preservation upgrade is required", table)
		}
	}
	return nil
}

// applySessionRebuild implements SQLite's documented table-rebuild procedure
// on an exclusively held connection. Foreign keys are restored on every exit;
// validation occurs before committing, so history cannot be cascaded away.
func (db *DB) applySessionRebuild(ctx context.Context, m migration) (err error) {
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	defer func() {
		_, restoreErr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA legacy_alter_table=OFF; PRAGMA foreign_keys=ON`)
		err = errors.Join(err, restoreErr)
	}()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF; PRAGMA legacy_alter_table=ON`); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	rows, err := tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE tbl_name='sessions' AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type,name`)
	if err != nil {
		return err
	}
	var retainedSchema []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			return errors.Join(err, rows.Close())
		}
		retainedSchema = append(retainedSchema, statement)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("rebuild sessions: %w", err)
	}
	for _, statement := range retainedSchema {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("restore session index or trigger: %w", err)
		}
	}
	rows, err = tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	invalid := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("store: session rebuild violates retained foreign keys")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, m.version, m.name, FormatTime(nowUTC())); err != nil {
		return err
	}
	return tx.Commit()
}

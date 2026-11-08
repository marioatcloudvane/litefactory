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
	if strings.HasPrefix(m.sql, sessionRebuildMarker) {
		return db.applyTableRebuild(ctx, m, []string{"sessions"})
	}
	if strings.HasPrefix(m.sql, tableRebuildMarker) {
		tables, err := rebuildTables(m.sql)
		if err != nil {
			return fmt.Errorf("store: migration %04d_%s: %w", m.version, m.name, err)
		}
		return db.applyTableRebuild(ctx, m, tables)
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

// sessionRebuildMarker is the original first-line marker of a sessions-only
// rebuild; tableRebuildMarker names the rebuilt tables after it.
const (
	sessionRebuildMarker = "-- litefactory:rebuild-sessions\n"
	tableRebuildMarker   = "-- litefactory:rebuild-tables "
)

// rebuildableTables is the complete list of tables a migration may rebuild
// through the protected procedure. Adding a table here is a reviewed decision.
var rebuildableTables = map[string]bool{
	"sessions":                  true,
	"durable_workflow_jobs":     true,
	"durable_workflow_attempts": true,
	"meter_attempts":            true,
	"application_results":       true,
}

// ErrRebuildTableRefused identifies a rebuild marker naming a table outside
// rebuildableTables.
var ErrRebuildTableRefused = errors.New("table is not rebuildable")

// rebuildTables reads the table names from a rebuild-tables marker line and
// refuses any name that is not on the allow-list, before anything runs.
func rebuildTables(body string) ([]string, error) {
	line, _, _ := strings.Cut(strings.TrimPrefix(body, tableRebuildMarker), "\n")
	tables := strings.Fields(line)
	if len(tables) == 0 {
		return nil, errors.New("rebuild marker names no table")
	}
	seen := make(map[string]bool, len(tables))
	for _, table := range tables {
		if !rebuildableTables[table] {
			return nil, fmt.Errorf("rebuild of %q refused: %w", table, ErrRebuildTableRefused)
		}
		if seen[table] {
			return nil, fmt.Errorf("rebuild marker names %q twice", table)
		}
		seen[table] = true
	}
	return tables, nil
}

// applyTableRebuild implements SQLite's documented table-rebuild procedure for
// tables on an exclusively held connection. Foreign keys are off so dropping a
// parent cascades nothing, and legacy_alter_table keeps child references naming
// the same table across the rename. Before committing it restores the captured
// indexes and triggers and refuses on any foreign key violation or any change
// in the row count of a rebuilt table or a table referencing one, so history
// cannot be lost silently. Pragmas are restored on every exit.
func (db *DB) applyTableRebuild(ctx context.Context, m migration, tables []string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("store: migration %04d_%s: %w", m.version, m.name, err)
		}
	}()
	for _, table := range tables {
		if !rebuildableTables[table] {
			return fmt.Errorf("rebuild of %q refused: %w", table, ErrRebuildTableRefused)
		}
	}
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
	retainedSchema, err := rebuildRetainedSchema(ctx, tx, tables)
	if err != nil {
		return err
	}
	counted, err := rebuildCountedTables(ctx, tx, tables)
	if err != nil {
		return err
	}
	before, err := rebuildRowCounts(ctx, tx, counted)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("rebuild %s: %w", strings.Join(tables, ", "), err)
	}
	for _, statement := range retainedSchema {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("restore index or trigger of rebuilt table: %w", err)
		}
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	invalid := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if invalid {
		return fmt.Errorf("rebuild of %s violates retained foreign keys", strings.Join(tables, ", "))
	}
	after, err := rebuildRowCounts(ctx, tx, counted)
	if err != nil {
		return err
	}
	for _, table := range counted {
		if before[table] != after[table] {
			return fmt.Errorf("rebuild changed the row count of %s from %d to %d", table, before[table], after[table])
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, m.version, m.name, FormatTime(nowUTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// rebuildRetainedSchema returns the explicit index and trigger SQL of tables,
// which dropping them removes and the rebuild restores.
func rebuildRetainedSchema(ctx context.Context, tx *sql.Tx, tables []string) ([]string, error) {
	var retained []string
	for _, table := range tables {
		rows, err := tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type,name`, table)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var statement string
			if err := rows.Scan(&statement); err != nil {
				return nil, errors.Join(err, rows.Close())
			}
			retained = append(retained, statement)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
	}
	return retained, nil
}

// rebuildCountedTables returns tables plus every existing table holding a
// foreign key to one of them.
func rebuildCountedTables(ctx context.Context, tx *sql.Tx, tables []string) ([]string, error) {
	rebuilt := make(map[string]bool, len(tables))
	counted := append([]string(nil), tables...)
	for _, table := range tables {
		rebuilt[table] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT m.name, lower(f."table") FROM sqlite_master m, pragma_foreign_key_list(m.name) f WHERE m.type='table' ORDER BY m.name`)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		if rebuilt[parent] && !rebuilt[child] && !seen[child] {
			seen[child] = true
			counted = append(counted, child)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return counted, nil
}

// rebuildRowCounts counts the rows of each table. Names come from
// sqlite_master or the allow-list, never from user input.
func rebuildRowCounts(ctx context.Context, tx *sql.Tx, tables []string) (map[string]int64, error) {
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&n); err != nil {
			return nil, fmt.Errorf("count rows of %s: %w", table, err)
		}
		counts[table] = n
	}
	return counts, nil
}

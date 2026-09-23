package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sqlite "modernc.org/sqlite"
)

// Test binaries migrate a fresh database hundreds of times, and under the race
// detector every one of the 60-odd migrations runs through instrumented,
// transpiled SQLite: about 4.5s per fresh store against 0.15s without -race.
// That fixed cost, paid by nearly every store-backed test, was most of the
// suite's wall time.
//
// So a test binary migrates for real once, into a template file keyed by the
// digest of the embedded migration set, and then restores every other empty
// database from that template page by page with SQLite's backup API. The
// result is the exact schema, and the exact schema_migrations rows, that the
// real sequence produced. Only a completely empty database qualifies: a test
// that plants a partial schema still runs the remaining migrations for real.
//
// Production binaries never take this path; testing.Testing reports false
// there. The template lives in the OS temp directory and is shared across test
// processes and runs. A changed migration changes the digest, and so the file.

// migrateFromTemplate gates the fast path. It is a variable so that a test of
// the migration sequence itself can opt out.
var migrateFromTemplate = testing.Testing()

var migrationTemplate struct {
	once sync.Once
	path string
	err  error
}

// restorer is the part of modernc.org/sqlite's connection that restores a
// database from another file.
type restorer interface {
	NewRestore(srcURI string) (*sqlite.Backup, error)
}

// migrateEmptyFromTemplate reports whether it restored db from the migration
// template. False with a nil error means db was not empty, and the caller
// migrates normally.
func (db *DB) migrateEmptyFromTemplate(ctx context.Context, migrations []migration) (bool, error) {
	var objects int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master`).Scan(&objects); err != nil {
		return false, fmt.Errorf("store: inspect schema: %w", err)
	}
	if objects != 0 {
		return false, nil
	}
	path, err := templatePath(ctx, migrations)
	if err != nil {
		return false, err
	}
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("store: restore migration template: %w", err)
	}
	defer conn.Close()
	err = conn.Raw(func(driverConn any) error {
		r, ok := driverConn.(restorer)
		if !ok {
			return errors.New("driver connection cannot restore")
		}
		backup, err := r.NewRestore("file:" + path + "?mode=ro")
		if err != nil {
			return err
		}
		_, stepErr := backup.Step(-1)
		return errors.Join(stepErr, backup.Finish())
	})
	if err != nil {
		return false, fmt.Errorf("store: restore migration template: %w", err)
	}
	return true, nil
}

// templatePath builds the template for this migration set at most once per
// process, and reuses a file an earlier process already built.
func templatePath(ctx context.Context, migrations []migration) (string, error) {
	migrationTemplate.once.Do(func() {
		migrationTemplate.path, migrationTemplate.err = buildTemplate(ctx, migrations)
	})
	return migrationTemplate.path, migrationTemplate.err
}

func buildTemplate(ctx context.Context, migrations []migration) (string, error) {
	h := sha256.New()
	for _, m := range migrations {
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00", m.version, m.name, m.sql)
	}
	path := filepath.Join(os.TempDir(), "litefactory-schema-"+hex.EncodeToString(h.Sum(nil))[:16]+".db")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Build under a private name and rename into place, so a concurrent test
	// process sees either no template or a complete one.
	dir, err := os.MkdirTemp("", "litefactory-schema-build-")
	if err != nil {
		return "", fmt.Errorf("store: build migration template: %w", err)
	}
	defer os.RemoveAll(dir)
	built := filepath.Join(dir, "template.db")
	sqlDB, err := sql.Open(driverName, "file:"+built+"?_pragma=foreign_keys(ON)")
	if err != nil {
		return "", fmt.Errorf("store: build migration template: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	tmpl := &DB{sql: sqlDB, path: built}
	err = tmpl.migrateAll(ctx, migrations)
	if closeErr := tmpl.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("store: build migration template: %w", err)
	}
	if err := os.Rename(built, path); err != nil {
		return "", fmt.Errorf("store: build migration template: %w", err)
	}
	return path, nil
}

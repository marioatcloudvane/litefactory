// Package store is litefactory's data layer: the SQLite database, its schema,
// and every accessor that reads or writes it. No SQL is written anywhere else
// in the tree.
//
// # Opening
//
// [Open] takes the database path — it does not resolve it. Path resolution
// belongs to internal/config, which owns $LF_HOME:
//
//	paths, err := config.Locate()
//	db, err := store.Open(ctx, paths.DBPath())
//	defer db.Close()
//	err = db.Migrate(ctx)
//
// [Open] does not migrate. Call [DB.Migrate] explicitly; it is idempotent, so
// every entry point that needs a schema may call it.
//
// # Conventions
//
// Every accessor takes a context as its first argument and is safe for
// concurrent use. The pool is capped at a single connection (SQLite is a file,
// not a server), so concurrent callers queue rather than contend.
//
// Nullable columns map to Go as follows, uniformly:
//
//   - Timestamps are [time.Time]. The zero time means SQL NULL. Times are
//     stored as UTC RFC 3339 text with millisecond precision (see [FormatTime]),
//     which sorts lexicographically in the same order it sorts chronologically,
//     so ORDER BY on a timestamp column is correct.
//   - Text columns whose empty value carries no meaning (a slug that has not
//     been frozen, a commit that was never made) are plain strings; "" means
//     SQL NULL.
//   - Forge-assigned identifiers are int64. Zero means SQL NULL, because no
//     forge ever issues id 0.
//   - Numbers whose zero is a real value — an exit code, a cost — are
//     pointers, so "exited 0" and "has not exited" stay distinguishable.
//
// Bookkeeping timestamps (created_at, updated_at, derived_at, recorded_at,
// lock_acquired_at) are set by the store from the wall clock. Timestamps that
// mean something to the domain — when the forge says a comment was written,
// when a run started — are always passed in by the caller.
//
// A read for a single row that is absent returns [ErrNotFound], except where a
// method documents a distinct "not yet" case and returns an ok bool instead.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// The pure-Go SQLite driver (D-7). It registers itself as "sqlite" — not
	// "sqlite3", which is the cgo package's name.
	_ "modernc.org/sqlite"
)

// driverName is the name modernc.org/sqlite registers with database/sql.
const driverName = "sqlite"

// DSN builds the connection string for the database at path.
//
// The pragmas are part of litefactory's correctness, not tuning:
//
//   - foreign_keys(ON) — SQLite defaults foreign key enforcement OFF, and the
//     setting is per connection. Without it here every REFERENCES clause in
//     the schema is decorative.
//   - journal_mode(WAL) — lets `litefactory logs` read while the daemon writes.
//   - busy_timeout(5000) — makes that concurrency wait rather than fail.
//   - synchronous(NORMAL) — the durability WAL is designed for.
func DSN(path string) string {
	return "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)"
}

// DB is a handle on litefactory's database. Create one with [Open]; the zero
// value is not usable. A DB is safe for concurrent use by multiple goroutines.
type DB struct {
	sql  *sql.DB
	path string
}

// Open connects to the SQLite database at path, creating the file if it does
// not exist. The containing directory must already exist — internal/config
// owns creating $LF_HOME.
//
// Open verifies that the connection really has foreign key enforcement on, so
// a driver or DSN regression surfaces here rather than as silent referential
// corruption months later. It does not apply migrations; call [DB.Migrate].
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store: open: empty database path")
	}
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("store: open %s: containing directory is not usable: %w", path, err)
	}

	sqlDB, err := sql.Open(driverName, DSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One connection. Every pragma above is per connection, WAL gives no
	// write concurrency anyway, and a single connection makes "the pragmas
	// are on" a property of the whole handle rather than of whichever
	// connection the pool happened to hand out.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sql: sqlDB, path: path}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("store: open %s: %w", path, err), db.Close())
	}
	if err := db.verifyForeignKeys(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// verifyForeignKeys fails unless the connection enforces foreign keys.
func (db *DB) verifyForeignKeys(ctx context.Context) error {
	var on int
	if err := db.sql.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&on); err != nil {
		return fmt.Errorf("store: read foreign_keys pragma: %w", err)
	}
	if on != 1 {
		return fmt.Errorf("store: foreign key enforcement is off for %s: the DSN pragmas did not take effect", db.path)
	}
	return nil
}

// Path returns the database file's path, for error messages and for the
// `daemon status` output.
func (db *DB) Path() string { return db.path }

// Close releases the underlying connection pool.
func (db *DB) Close() error {
	if err := db.sql.Close(); err != nil {
		return fmt.Errorf("store: close %s: %w", db.path, err)
	}
	return nil
}

// withTx runs fn inside a transaction, committing when it returns nil and
// rolling back otherwise.
//
// The rollback is driven by a committed flag rather than by recover(), so it
// also fires while a panic unwinds through this frame — a panicking caller
// cannot leave the single connection pinned to an open transaction. The tree
// contains exactly one recover, in internal/redact, and this is deliberately
// not a second one.
func (db *DB) withTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rbErr := tx.Rollback()
		if err != nil && rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// timeLayout is how every timestamp column is stored: UTC, RFC 3339, fixed
// width, millisecond precision. Fixed width matters — it is what makes string
// comparison agree with chronological comparison, so ORDER BY and MAX() work
// on these columns.
const timeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime renders t the way the database stores it: UTC, RFC 3339, three
// decimal places. It is exported because the same instants are written to
// NNNN.meta.json (S-3), which must agree with the database.
func FormatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// ParseTime reads a timestamp written by [FormatTime]. It also accepts a plain
// RFC 3339 string, so a value hand-edited into the database still loads.
func ParseTime(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// nowUTC is the store's clock for bookkeeping columns.
func nowUTC() time.Time { return time.Now().UTC() }

// nullTime converts a Go time to a nullable column value: the zero time is
// SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return FormatTime(t)
}

// nullString converts a Go string to a nullable column value: "" is SQL NULL.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullID converts a forge-assigned identifier to a nullable column value: 0 is
// SQL NULL, because no forge issues id 0.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// scanTime converts a nullable timestamp column into a Go time; NULL and
// unparseable values both yield the zero time, with the parse failure
// reported.
func scanTime(v sql.NullString) (time.Time, error) {
	if !v.Valid || v.String == "" {
		return time.Time{}, nil
	}
	return ParseTime(v.String)
}

// scanID converts a nullable identifier column into an int64; NULL is 0.
func scanID(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

// scanInt converts a nullable integer column into a pointer, keeping NULL and
// zero distinct.
func scanInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// scanFloat converts a nullable real column into a pointer, keeping NULL and
// zero distinct.
func scanFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

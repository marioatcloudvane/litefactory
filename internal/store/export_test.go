package store

import "testing"

// MigrateBefore stands db at the schema immediately preceding the named
// migration so external tests can build pre-migration history.
func MigrateBefore(t *testing.T, db *DB, name string) {
	t.Helper()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, name))
}

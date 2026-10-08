// Package storetest provides SQLite fault fixtures for recovery lifecycle tests.
package storetest

import (
	"database/sql"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// FailRecoveryWrite refuses resolved checkpoints or deletion until released.
func FailRecoveryWrite(t *testing.T, db *store.DB, deletion bool) func() {
	t.Helper()
	raw, err := sql.Open("sqlite", store.DSN(db.Path()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Close(); err != nil {
			t.Error(err)
		}
	})
	query := `CREATE TRIGGER recovery_test_fault BEFORE UPDATE ON report_recoveries WHEN NEW.state='resolved' BEGIN SELECT RAISE(ABORT,'checkpoint fault'); END`
	if deletion {
		query = `CREATE TRIGGER recovery_test_fault BEFORE DELETE ON report_recoveries BEGIN SELECT RAISE(ABORT,'delete fault'); END`
	}
	if _, err := raw.ExecContext(t.Context(), query); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if _, err := raw.ExecContext(t.Context(), `DROP TRIGGER recovery_test_fault`); err != nil {
			t.Fatal(err)
		}
	}
}

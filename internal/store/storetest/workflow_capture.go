package storetest

import (
	"database/sql"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// FailWorkflowCaptureWrite injects a retention failure before graph execution.
func FailWorkflowCaptureWrite(t *testing.T, db *store.DB) {
	t.Helper()
	raw, err := sql.Open("sqlite", store.DSN(db.Path()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := raw.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := raw.ExecContext(t.Context(), `CREATE TRIGGER refuse_capture BEFORE INSERT ON research_launch_definitions BEGIN SELECT RAISE(ABORT,'capture persistence fault'); END`); err != nil {
		t.Fatal(err)
	}
}

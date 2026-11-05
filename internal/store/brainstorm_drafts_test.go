package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestBrainstormDraftSetReadsMissingAndPurposeColumn(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.LatestBrainstormDraftSet(t.Context(), "brainstorm-x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("latest = %v", err)
	}
	if _, err = db.ReadBrainstormDraftSet(t.Context(), "set-x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read = %v", err)
	}
	if _, err = db.ReadBrainstormDraft(t.Context(), "draft-x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft = %v", err)
	}
	var n int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM pragma_table_info('brainstorm_scans') WHERE name='purpose' AND "notnull"=0`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("purpose column = %d %v", n, err)
	}
	if brainstormPurposeColumn("") != nil || brainstormPurposeColumn(BrainstormPurposeQuestion) != nil || brainstormPurposeColumn(BrainstormPurposeDrafts) != BrainstormPurposeDrafts {
		t.Fatal("question purpose must be stored as NULL")
	}
}

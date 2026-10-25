package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestPreparationCaveatsDurableAndBounded(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 1154)
	run := testRun(t, db, session.ID, 1154)
	if len(run.PreparationCaveats) != 0 {
		t.Fatal("new run invents caveats")
	}
	want := []string{"pending refinement", "optional context unavailable"}
	if _, err := db.RecordPreparationCaveats(t.Context(), run.ID, append(append([]string{}, want...), want[0])); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.PreparationCaveats, want) {
		t.Fatalf("lost or duplicated caveats: %v", got.PreparationCaveats)
	}
	for _, bad := range [][]string{{""}, {strings.Repeat("x", 1025)}, make([]string, 17)} {
		if _, err := db.RecordPreparationCaveats(t.Context(), run.ID, bad); err == nil {
			t.Fatal("unbounded caveat accepted")
		}
	}
	got, err = db.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.PreparationCaveats, want) {
		t.Fatal("invalid write changed retained conditions")
	}
}

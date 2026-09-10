package store

import (
	"errors"
	"testing"
)

func TestModeSelectionIsStableAndCleanupIsRecoverable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 32)
	first, err := db.GetOrCreateModeSelection(t.Context(), ModeSelection{SessionID: session.ID, TriggerCommentID: 99, Mode: "plan", Label: "Mode: Plan"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.GetOrCreateModeSelection(t.Context(), ModeSelection{SessionID: session.ID, TriggerCommentID: 99, Mode: "spike", Label: "Mode: Spike"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Mode != first.Mode || again.Label != first.Label {
		t.Fatalf("stored selection did not win: %+v", again)
	}
	if err := db.MarkModeLabelApplied(t.Context(), session.ID, 99); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkModeCleanupPending(t.Context(), session.ID, 99); err != nil {
		t.Fatal(err)
	}
	pending, ok, err := db.PendingModeCleanup(t.Context(), session.ID)
	if err != nil || !ok || pending.Mode != "plan" {
		t.Fatalf("pending = %+v, %t, %v", pending, ok, err)
	}
	if err := db.DeleteModeSelection(t.Context(), session.ID, 99); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.PendingModeCleanup(t.Context(), session.ID); err != nil || ok {
		t.Fatalf("cleanup remains: %t, %v", ok, err)
	}
}

func TestModeRetirementPreservesUnfinishedBootstrapTrigger(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "unconsumed"
		if pending {
			name = "explicitly-completed"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 33)
			if _, _, err := db.BootstrapWatermark(t.Context(), session.ID, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := db.GetOrCreateModeSelection(t.Context(), ModeSelection{SessionID: session.ID, TriggerCommentID: 0, Mode: "plan", Label: "Mode: Plan"}); err != nil {
				t.Fatal(err)
			}
			if pending {
				if err := db.MarkModeCleanupPending(t.Context(), session.ID, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.RetireCompletedModeSelections(t.Context()); err != nil {
				t.Fatal(err)
			}
			row, err := db.GetModeSelection(t.Context(), session.ID, 0)
			if pending {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("completed row=%+v error=%v", row, err)
				}
			} else if err != nil || row.Mode != "plan" {
				t.Fatalf("unfinished bootstrap selection lost: %+v %v", row, err)
			}
		})
	}
}

func TestModeRetirementEmptyJournal(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	for range 2 {
		if err := db.RetireCompletedModeSelections(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

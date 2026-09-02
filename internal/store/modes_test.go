package store

import "testing"

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

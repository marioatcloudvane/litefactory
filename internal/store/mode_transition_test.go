package store

import (
	"errors"
	"testing"
)

func TestModeTransitionRefinementRetirementUsesIndependentCompletion(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		name := "pending"
		if consumed {
			name = "consumed"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 123)
			if _, _, err := db.BootstrapWatermark(t.Context(), session.ID, 100); err != nil {
				t.Fatal(err)
			}
			if err := db.AdmitStoryRefinement(t.Context(), StoryRefinement{SessionID: session.ID, CommentID: 60, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}); err != nil {
				t.Fatal(err)
			}
			selection := ModeSelection{SessionID: session.ID, TriggerCommentID: 60, Mode: "plan", Label: "Mode: Plan", PreviousLabel: "Mode: Implement"}
			if _, err := db.GetOrCreateModeSelection(t.Context(), selection); err != nil {
				t.Fatal(err)
			}
			if consumed {
				if err := db.ConsumeStoryRefinement(t.Context(), session.ID, 60); err != nil {
					t.Fatal(err)
				}
			}
			row, pending, err := db.PendingModeCleanup(t.Context(), session.ID)
			if err != nil || pending != consumed {
				t.Fatalf("cleanup=%+v pending=%t err=%v", row, pending, err)
			}
			if err := db.RetireCompletedModeSelections(t.Context()); err != nil {
				t.Fatal(err)
			}
			row, err = db.GetModeSelection(t.Context(), session.ID, 60)
			if consumed {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("consumed journal=%+v err=%v", row, err)
				}
			} else if err != nil || row.PreviousLabel != selection.PreviousLabel || row.Mode != selection.Mode {
				t.Fatalf("pending journal lost=%+v err=%v", row, err)
			}
		})
	}
}

package store

import (
	"strings"
	"testing"
)

func TestValidateReturnKeepsExactInputAndSelectionOnReplay(t *testing.T) {
	db := newTestDB(t)
	input := ValidateReturn{InstanceID: 7, Parent: "github:acme/api#497", Generation: 1, Commit: strings.Repeat("a", 40), ReportRevision: "report-r1", CommentID: 101, AuthorID: 42, Explanation: "Change the shared formatter.", Context: `{"review":"full retained assessment"}`, CreatedAt: graphTestTime()}
	first, err := db.PutValidateReturn(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := db.SelectValidateReturn(t.Context(), 7, `[{"story":"story-002"}]`, "named-story")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyValidateReturn(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	replay, err := db.PutValidateReturn(t.Context(), input)
	if err != nil || !replay.Applied || replay.Selection != selected.Selection || replay.Context != first.Context {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err := db.SelectValidateReturn(t.Context(), 7, `[{"story":"story-001"}]`, "agent"); err == nil {
		t.Fatal("saved dispatch selection was replaced")
	}
	changed := input
	changed.Explanation = "Edited explanation"
	if _, err := db.PutValidateReturn(t.Context(), changed); err == nil {
		t.Fatal("edited replay replaced admitted owner input")
	}
	retained, err := db.GetValidateReturn(t.Context(), 7)
	if err != nil || retained.Explanation != input.Explanation || retained.Selection != selected.Selection {
		t.Fatalf("original return lost %+v %v", retained, err)
	}
}

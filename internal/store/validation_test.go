package store

import "testing"

func TestValidationAssessmentRetainsExactReplayAndRejectsReplacement(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 195)
	run := testRun(t, db, s.ID, 1)
	a := ValidationAssessment{RunID: run.ID, Parent: s.TaskRef, Generation: 1, ContextJSON: `{"snapshot":"original"}`, ResultJSON: `{"status":"unverified"}`}
	if err := db.RetainValidationAssessment(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainValidationAssessment(t.Context(), a); err != nil {
		t.Fatal("idempotent replay", err)
	}
	got, err := db.GetValidationAssessment(t.Context(), run.ID)
	if err != nil || got != a {
		t.Fatal("retained input/result changed", got, err)
	}
	changed := a
	changed.ResultJSON = `{"status":"satisfied"}`
	if err := db.RetainValidationAssessment(t.Context(), changed); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	got, err = db.GetValidationAssessment(t.Context(), run.ID)
	if err != nil || got != a {
		t.Fatal("original overwritten", got, err)
	}
}

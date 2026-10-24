package store

import (
	"errors"
	"testing"
	"time"
)

func TestAuthoringMeterIndependentOwnership(t *testing.T) {
	db := newTestDB(t)
	session := authoringTestSession(t, db)
	_, err := db.BeginStandardsAuthoringAttempt(t.Context(), StandardsAuthoringAttempt{ID: "meter-attempt", Token: "meter-token", SessionID: session.ID, InputVersion: session.Version, LeaseUntil: time.Now().UTC().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, e := db.StartAuthoringMeterAttempt(t.Context(), session.ID, "claude", "balanced", "prepared-digest", "meter-attempt", "wrong-token"); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("stale provider launch accepted: %v", e)
	}
	a, err := db.StartAuthoringMeterAttempt(t.Context(), session.ID, "claude", "balanced", "prepared-digest", "meter-attempt", "meter-token")
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMeterAttempt(t.Context(), a.ID)
	if err != nil || got.AuthoringSessionID != session.ID || got.RunID != 0 || got.ChatRepoID != 0 || got.DoctorAttemptID != "" || got.JudgeAttemptID != "" {
		t.Fatalf("wrong attribution: %+v %v", got, err)
	}
	attempts, e := db.ListStandardsAuthoringAttempts(t.Context(), session.ID)
	if e != nil || len(attempts) != 1 || attempts[0].Provider != "claude" || attempts[0].Model != "balanced" || attempts[0].UsageReference != a.ID || attempts[0].InputDigest != "prepared-digest" {
		t.Fatalf("provider input not retained before launch: %+v %v", attempts, e)
	}
	if err = db.FinishMeterAttempt(t.Context(), a.ID, "failure", "unknown", "", "", nil); err != nil {
		t.Fatal(err)
	}
	snap, err := db.NewMeterSnapshot(t.Context(), MeterFilter{Mode: "standards_authoring"}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := snap.Close(t.Context()); e != nil {
			t.Error(e)
		}
	}()
	rows, err := snap.Next(t.Context(), 100)
	if err != nil || len(rows) != 1 || rows[0].ExecutionPath != "standards_authoring" || rows[0].RunID != 0 {
		t.Fatalf("incorrect ledger view: %+v %v", rows, err)
	}
	if _, err = db.StartAuthoringMeterAttempt(t.Context(), "missing", "claude", "balanced", "prepared-digest", "meter-attempt", "meter-token"); err == nil {
		t.Fatal("unknown session accepted")
	}
}

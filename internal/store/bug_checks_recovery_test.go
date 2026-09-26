package store

import (
	"reflect"
	"testing"
	"time"
)

func TestBugCheckRecoveryPreservesCompletedEvidenceAndAppendsProofOnce(t *testing.T) {
	db := newTestDB(t)
	b := checkPersistenceBinding(t, db)
	checkPersistenceEnsure(t, db, b)
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	observation := checkPersistencePassed()
	observation.Outcome = "blocked"
	observation.Reason = "cleanup uncertain"
	observation.CleanupConfirmed = false
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, observation); err != nil {
		t.Fatal(err)
	}
	when := observation.Ended.Add(time.Second)
	for range 2 {
		if err := db.ReconcileBugCheckStopped(t.Context(), b.SessionID, b.Operation, 0, observation.ProcessIdentity, when); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.GetBugCheckPhase(t.Context(), b.SessionID, b.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cleanup || !reflect.DeepEqual(got.Commands[0].Observation, observation) {
		t.Fatalf("cleanup reconciliation rewrote historical command: %+v", got)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM bug_check_events WHERE session_id=? AND operation=? AND kind='namespace_stopped'`, b.SessionID, b.Operation).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("cleanup proof events=%d", count)
	}
	for range 2 {
		if err = db.ReleaseQuiescentBugCheckWork(t.Context(), b.SessionID, b.Operation); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "next-fixer", "repair"); err != nil || !ok {
		t.Fatalf("guard remained after cleanup: %v %v", ok, err)
	}
	if err = db.ReleaseQuiescentBugCheckWork(t.Context(), b.SessionID, b.Operation); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "intruder", "checks"); err != nil || ok {
		t.Fatalf("replay removed foreign guard: %v %v", ok, err)
	}
}

func TestBugCheckRecoveryRequiresMatchingLaunchAndPreservesChildGuard(t *testing.T) {
	db := newTestDB(t)
	b := checkPersistenceBinding(t, db)
	checkPersistenceEnsure(t, db, b)
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := db.ReconcileBugCheckStopped(t.Context(), b.SessionID, b.Operation, 0, "unobserved", time.Now()); err == nil {
		t.Fatal("unknown launch certified stopped")
	}
	if err := db.ReleaseQuiescentBugCheckWork(t.Context(), b.SessionID, b.Operation); err == nil {
		t.Fatal("unknown launch guard released")
	}
	observation := checkPersistencePassed()
	observation.Outcome = ""
	observation.Ended = time.Time{}
	observation.CleanupConfirmed = false
	observation.Exit = nil
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, false, observation); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"", "other-start"} {
		if err := db.ReconcileBugCheckStopped(t.Context(), b.SessionID, b.Operation, 0, identity, time.Now()); err == nil {
			t.Errorf("invalid process identity %q accepted", identity)
		}
	}
	if err := db.ReconcileBugCheckStopped(t.Context(), b.SessionID, b.Operation, 0, observation.ProcessIdentity, observation.Started.Add(-time.Second)); err == nil {
		t.Fatal("pre-launch stopped observation accepted")
	}
	if err := db.ReconcileBugCheckStopped(t.Context(), b.SessionID, b.Operation, 0, observation.ProcessIdentity, observation.Started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetBugCheckPhase(t.Context(), b.SessionID, b.Operation)
	if err != nil {
		t.Fatal(err)
	}
	recovered := got.Commands[0].Observation
	if recovered.Outcome != "blocked" || recovered.Exit != nil || recovered.CaptureComplete || !got.Cleanup {
		t.Fatalf("interruption became successful: %+v", got)
	}
	if err = db.ReleaseBugWork(t.Context(), b.SessionID, b.Operation); err != nil {
		t.Fatal(err)
	}
	// Even an identically named child owner is outside this release method's authority.
	if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, b.Operation, "child"); err != nil || !ok {
		t.Fatalf("child claim: %v %v", ok, err)
	}
	if err = db.ReleaseQuiescentBugCheckWork(t.Context(), b.SessionID, b.Operation); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "intruder", "repair"); err != nil || ok {
		t.Fatalf("child guard released: %v %v", ok, err)
	}
}

package store

import (
	"testing"
	"time"
)

// TestBugFixtureApprovedAnalysisJourney exercises reservation through owner
// approval end to end, pinning the state a repair reservation depends on.
func TestBugFixtureApprovedAnalysisJourney(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, identity := bugFixtureSession(t, db, 401)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	analystAttempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))

	revision, approval := bugFixtureApprovedAnalysis(t, db, session, identity, instance, analystAttempt, run, bugFixtureTime(time.Minute))

	if revision.Revision != 1 || !revision.ReviewReady {
		t.Fatalf("revision = %+v, want revision 1 and review-ready", revision)
	}
	if approval.Review != "review-1" || approval.OwnerID != identity.OwnerID {
		t.Fatalf("approval = %+v", approval)
	}

	state, current, curApproval, err := db.CurrentBugAnalysis(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("current bug analysis: %v", err)
	}
	if state.CurrentAnalysis != 1 || current.Revision != 1 {
		t.Fatalf("state = %+v, current = %+v", state, current)
	}
	if curApproval == nil || curApproval.Review != "review-1" {
		t.Fatalf("current approval = %+v", curApproval)
	}
}

// TestReserveBugAnalysisPass_ReplayReturnsOriginalState pins that an exact
// replay of the same invocation never spends a second pass slot.
func TestReserveBugAnalysisPass_ReplayReturnsOriginalState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 402)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	attempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))

	digest := submissionDigest([]byte("input-1"))
	in := BugAnalysisPass{SessionID: session.ID, Invocation: "inv-1", InputDigest: digest, GraphAttemptID: attempt.ID}
	first, err := db.ReserveBugAnalysisPass(t.Context(), in, bugCurrentRevision(t, db, session.ID))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if first.Ordinal != 1 {
		t.Fatalf("ordinal = %d, want 1", first.Ordinal)
	}

	// Replay with a stale expected revision: the exact same invocation must
	// still return its already-recorded state, not a revision conflict.
	second, err := db.ReserveBugAnalysisPass(t.Context(), in, 999)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second != first {
		t.Fatalf("replay = %+v, want identical to first reservation %+v", second, first)
	}

	state, _, _, err := db.CurrentBugAnalysis(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.AnalysisUsed != 1 {
		t.Fatalf("analysis_used = %d, want 1: replay spent a second slot", state.AnalysisUsed)
	}

	// A genuinely different input for the same invocation is a replay conflict.
	conflicting := in
	conflicting.InputDigest = submissionDigest([]byte("different-input"))
	if _, err := db.ReserveBugAnalysisPass(t.Context(), conflicting, bugCurrentRevision(t, db, session.ID)); err == nil {
		t.Fatal("changed input for the same invocation was accepted")
	}
}

// TestClaimBugAnalysisDispatch_ReplayDoesNotRelaunch pins that a second claim
// for an already-dispatched invocation reports no fresh launch.
func TestClaimBugAnalysisDispatch_ReplayDoesNotRelaunch(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 403)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	attempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))

	digest := submissionDigest([]byte("input-1"))
	if _, err := db.ReserveBugAnalysisPass(t.Context(), BugAnalysisPass{SessionID: session.ID, Invocation: "inv-1", InputDigest: digest, GraphAttemptID: attempt.ID}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	first, err := db.ClaimBugAnalysisDispatch(t.Context(), session.ID, "inv-1", digest, run.ID)
	if err != nil || !first {
		t.Fatalf("first claim: claimed=%v err=%v", first, err)
	}
	second, err := db.ClaimBugAnalysisDispatch(t.Context(), session.ID, "inv-1", digest, run.ID)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if second {
		t.Fatal("replay claim reported a fresh launch")
	}
}

// TestFinishBugAnalysisPass_TerminalStateIsImmutable pins that a terminal pass
// outcome cannot be rewritten to a different outcome.
func TestFinishBugAnalysisPass_TerminalStateIsImmutable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 404)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	attempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))
	digest := submissionDigest([]byte("input-1"))
	if _, err := db.ReserveBugAnalysisPass(t.Context(), BugAnalysisPass{SessionID: session.ID, Invocation: "inv-1", InputDigest: digest, GraphAttemptID: attempt.ID}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimBugAnalysisDispatch(t.Context(), session.ID, "inv-1", digest, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugAnalysisPass(t.Context(), session.ID, "inv-1", "failed", "agent crashed"); err != nil {
		t.Fatalf("finish failed: %v", err)
	}
	if err := db.FinishBugAnalysisPass(t.Context(), session.ID, "inv-1", "failed", "agent crashed"); err != nil {
		t.Fatalf("identical replay of a terminal pass was refused: %v", err)
	}
	if err := db.FinishBugAnalysisPass(t.Context(), session.ID, "inv-1", "uncertain", "agent crashed"); err == nil {
		t.Fatal("terminal pass accepted a different outcome")
	}
	got, err := db.GetBugAnalysisPass(t.Context(), session.ID, "inv-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" {
		t.Fatalf("state = %q, want it to stay failed", got.State)
	}
}

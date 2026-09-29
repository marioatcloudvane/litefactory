package store

import (
	"testing"
	"time"
)

// bugExhaustionFixture drives the three base repair attempts to failure, ends
// the owning graph instance and returns a session whose current attempt node
// is exhausted and ready for RecordBugExhaustion/DecideBugExhaustion.
type bugExhaustionFixture struct {
	DB              *DB
	Session         Session
	Identity        BugIdentity
	Instance        GraphInstance
	Revision        BugAnalysisRevision
	Approval        BugAnalysisApproval
	FailedAttemptID int64
}

func newBugExhaustionFixture(t *testing.T, issue int) bugExhaustionFixture {
	t.Helper()
	db := newTestDB(t)
	session, identity := bugFixtureSession(t, db, issue)
	parentDef := bugFixtureDefinition(t, db, "a")
	childDef := bugFixtureDefinition(t, db, "c")
	instance := bugFixtureInstance(t, db, session, parentDef, "analyst", bugFixtureTime(0))
	bugFixtureLifecycle(t, db, session, instance, childDef)

	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	analystAttempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))
	revision, approval := bugFixtureApprovedAnalysis(t, db, session, identity, instance, analystAttempt, run, bugFixtureTime(0))

	var fixerAttempt GraphNodeAttempt
	for i, cause := range []string{"initial", "test_failure", "test_failure"} {
		fixerAttempt = bugFixtureAttempt(t, db, instance, "attempt", 0, bugFixtureTime(0))
		request := "repair-" + string(rune('1'+i))
		in := bugFixtureRepairRequest(session, revision, approval, request, cause, fixerAttempt.ID, "")
		if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); err != nil {
			t.Fatalf("reserve ordinal %d: %v", i+1, err)
		}
		if err := db.StopBugRepair(t.Context(), session.ID, request, "failed", "project checks failed", true); err != nil {
			t.Fatalf("stop ordinal %d: %v", i+1, err)
		}
		final := ""
		if i == 2 {
			final = GraphFailed
		}
		completion := GraphCompletion{Status: GraphFailed, FinishedAt: bugFixtureTime(0)}
		if final != "" {
			completion.InstanceStatus = GraphFailed
		}
		if err := db.CompleteGraphNodeAttempt(t.Context(), fixerAttempt.ID, completion); err != nil {
			t.Fatalf("complete fixer attempt %d: %v", i+1, err)
		}
	}

	return bugExhaustionFixture{DB: db, Session: session, Identity: identity, Instance: instance, Revision: revision, Approval: approval, FailedAttemptID: fixerAttempt.ID}
}

// TestRecordBugExhaustion_SequentialNumberingAndUndecidedGuard is AC-003: each
// exhaustion for a bug is sequentially numbered, a replay for the same source
// attempt returns its already-recorded number, and a fresh exhaustion is
// refused while an earlier one has no decision yet.
func TestRecordBugExhaustion_SequentialNumberingAndUndecidedGuard(t *testing.T) {
	t.Parallel()
	f := newBugExhaustionFixture(t, 420)

	n, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, f.FailedAttemptID)
	if err != nil {
		t.Fatalf("record exhaustion: %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d, want 1", n)
	}

	// Replay for the exact same source attempt returns the same number, not a
	// second row.
	again, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, f.FailedAttemptID)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again != 1 {
		t.Fatalf("replay n = %d, want 1", again)
	}

	history, err := f.DB.BugRepairHistory(t.Context(), f.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Exhaustions) != 1 || history.Exhaustions[0].N != 1 || history.Exhaustions[0].Decision != "" {
		t.Fatalf("exhaustion history = %+v, want one undecided exhaustion n=1", history.Exhaustions)
	}
	if len(history.Attempts) != 3 {
		t.Fatalf("repair history holds %d attempts, want 3 (bounded lifetime attempts)", len(history.Attempts))
	}

	// A distinct source attempt cannot record a second exhaustion while the
	// first is undecided. RecordBugExhaustion does not otherwise constrain
	// which graph attempt names the exhausted outcome, so any other valid
	// attempt identity demonstrates the undecided-guard on its own.
	other := graphTestAttempt(t, f.DB, graphTestInstance(t, f.DB), 0)
	if _, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, other.ID); err == nil {
		t.Fatal("a second exhaustion was recorded while the first stayed undecided")
	}
}

// TestDecideBugExhaustion_StopSetsTerminalDisposition covers the "stop" half
// of AC-005/AC-010: the decision is recorded once, and the lifecycle moves to
// the terminal owner-stopped disposition in the same transaction.
func TestDecideBugExhaustion_StopSetsTerminalDisposition(t *testing.T) {
	t.Parallel()
	f := newBugExhaustionFixture(t, 421)
	if _, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, f.FailedAttemptID); err != nil {
		t.Fatal(err)
	}

	req := BugExhaustionDecisionRequest{
		SessionID: f.Session.ID, N: 1, Decision: "stop", CommentID: 9001, AuthorID: f.Identity.OwnerID,
		ExpectedRevision: bugCurrentRevision(t, f.DB, f.Session.ID), At: bugFixtureTime(time.Minute),
	}
	out, err := f.DB.DecideBugExhaustion(t.Context(), req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if out.Decision != "stop" || out.Successor != (GraphInstance{}) {
		t.Fatalf("stop outcome = %+v, want no successor", out)
	}

	life, err := f.DB.GetBugLifecycle(t.Context(), f.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if life.Disposition != "owner-stopped" {
		t.Fatalf("disposition = %q, want owner-stopped", life.Disposition)
	}

	// Replay of the exact same decision is a no-op, not a second effect.
	replay, err := f.DB.DecideBugExhaustion(t.Context(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay != out {
		t.Fatalf("replay = %+v, want identical %+v", replay, out)
	}

	// A distinct comment deciding the same exhaustion again is refused: exactly
	// one decision per exhaustion.
	different := req
	different.CommentID = 9002
	if _, err := f.DB.DecideBugExhaustion(t.Context(), different); err == nil {
		t.Fatal("a second decision was accepted for an already-decided exhaustion")
	}
}

// TestDecideBugExhaustion_GrantClaimsContinuationAndReserveBugRepair covers
// AC-006/AC-012: a grant decision claims exactly one budget-grant continuation
// atomically, and ReserveBugRepair only accepts ordinal 4 with that unused,
// matching grant reference — a reused reference fails, and reusing the wrong
// one fails too.
func TestDecideBugExhaustion_GrantClaimsContinuationAndReserveBugRepair(t *testing.T) {
	t.Parallel()
	f := newBugExhaustionFixture(t, 422)
	if _, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, f.FailedAttemptID); err != nil {
		t.Fatal(err)
	}

	req := BugExhaustionDecisionRequest{
		SessionID: f.Session.ID, N: 1, Decision: "grant", Instructions: "focus on the retry path",
		CommentID: 9101, AuthorID: f.Identity.OwnerID,
		ExpectedRevision: bugCurrentRevision(t, f.DB, f.Session.ID), At: bugFixtureTime(time.Minute),
	}
	out, err := f.DB.DecideBugExhaustion(t.Context(), req)
	if err != nil {
		t.Fatalf("decide grant: %v", err)
	}
	if out.Successor.ID == 0 || out.Successor.CurrentNode != "attempt" || out.Successor.Status != GraphRunning {
		t.Fatalf("grant successor = %+v, want a fresh running instance parked on attempt", out.Successor)
	}

	// Replay is idempotent: the same successor, not a second continuation.
	replay, err := f.DB.DecideBugExhaustion(t.Context(), req)
	if err != nil {
		t.Fatalf("replay grant: %v", err)
	}
	if replay.Successor.ID != out.Successor.ID {
		t.Fatalf("replay successor = %d, want %d", replay.Successor.ID, out.Successor.ID)
	}

	life, err := f.DB.GetBugLifecycle(t.Context(), f.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if life.ParentInstance != out.Successor.ID || life.Disposition != "running" {
		t.Fatalf("lifecycle = %+v, want it resumed onto the successor", life)
	}

	granted := bugFixtureAttempt(t, f.DB, out.Successor, "attempt", 0, bugFixtureTime(2*time.Minute))
	fourth := bugFixtureRepairRequest(f.Session, f.Revision, f.Approval, "repair-4", "test_failure", granted.ID, "exhaustion:1")

	// A grant reference is required and unique: an unrelated reference is
	// refused outright.
	wrongRef := fourth
	wrongRef.GrantReference = "exhaustion:99"
	if _, err := f.DB.ReserveBugRepair(t.Context(), wrongRef, bugCurrentRevision(t, f.DB, f.Session.ID)); err == nil {
		t.Fatal("an unknown grant reference was accepted")
	}

	reserved, err := f.DB.ReserveBugRepair(t.Context(), fourth, bugCurrentRevision(t, f.DB, f.Session.ID))
	if err != nil {
		t.Fatalf("reserve granted ordinal 4: %v", err)
	}
	if reserved.Ordinal != 4 {
		t.Fatalf("ordinal = %d, want 4", reserved.Ordinal)
	}

	// The same grant reference cannot pay for a second reservation.
	reuse := bugFixtureRepairRequest(f.Session, f.Revision, f.Approval, "repair-4b", "test_failure", granted.ID, "exhaustion:1")
	if err := f.DB.StopBugRepair(t.Context(), f.Session.ID, "repair-4", "failed", "still broken", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.ReserveBugRepair(t.Context(), reuse, bugCurrentRevision(t, f.DB, f.Session.ID)); err == nil {
		t.Fatal("a spent grant reference was accepted a second time")
	}
}

// TestBugRepeatedGrantCycles pins that granting is uncapped: a second
// exhaustion, after the granted fourth attempt itself fails, can be granted
// again to reach a fifth reservation.
func TestBugRepeatedGrantCycles(t *testing.T) {
	t.Parallel()
	f := newBugExhaustionFixture(t, 423)
	if _, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, f.FailedAttemptID); err != nil {
		t.Fatal(err)
	}
	firstGrant := BugExhaustionDecisionRequest{
		SessionID: f.Session.ID, N: 1, Decision: "grant", CommentID: 9201, AuthorID: f.Identity.OwnerID,
		ExpectedRevision: bugCurrentRevision(t, f.DB, f.Session.ID), At: bugFixtureTime(time.Minute),
	}
	firstOut, err := f.DB.DecideBugExhaustion(t.Context(), firstGrant)
	if err != nil {
		t.Fatalf("first grant: %v", err)
	}

	fourthAttempt := bugFixtureAttempt(t, f.DB, firstOut.Successor, "attempt", 0, bugFixtureTime(2*time.Minute))
	fourth := bugFixtureRepairRequest(f.Session, f.Revision, f.Approval, "repair-4", "test_failure", fourthAttempt.ID, "exhaustion:1")
	if _, err := f.DB.ReserveBugRepair(t.Context(), fourth, bugCurrentRevision(t, f.DB, f.Session.ID)); err != nil {
		t.Fatalf("reserve ordinal 4: %v", err)
	}
	if err := f.DB.StopBugRepair(t.Context(), f.Session.ID, "repair-4", "failed", "still broken", true); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.CompleteGraphNodeAttempt(t.Context(), fourthAttempt.ID, GraphCompletion{Status: GraphFailed, FinishedAt: bugFixtureTime(2 * time.Minute), InstanceStatus: GraphFailed}); err != nil {
		t.Fatal(err)
	}

	secondN, err := f.DB.RecordBugExhaustion(t.Context(), f.Session.ID, fourthAttempt.ID)
	if err != nil {
		t.Fatalf("second exhaustion: %v", err)
	}
	if secondN != 2 {
		t.Fatalf("second exhaustion n = %d, want 2", secondN)
	}

	secondGrant := BugExhaustionDecisionRequest{
		SessionID: f.Session.ID, N: 2, Decision: "grant", CommentID: 9301, AuthorID: f.Identity.OwnerID,
		ExpectedRevision: bugCurrentRevision(t, f.DB, f.Session.ID), At: bugFixtureTime(3 * time.Minute),
	}
	secondOut, err := f.DB.DecideBugExhaustion(t.Context(), secondGrant)
	if err != nil {
		t.Fatalf("second grant: %v", err)
	}
	if secondOut.Successor.ID == firstOut.Successor.ID {
		t.Fatal("second grant reused the first successor instance")
	}

	fifthAttempt := bugFixtureAttempt(t, f.DB, secondOut.Successor, "attempt", 0, bugFixtureTime(4*time.Minute))
	fifth := bugFixtureRepairRequest(f.Session, f.Revision, f.Approval, "repair-5", "test_failure", fifthAttempt.ID, "exhaustion:2")
	reserved, err := f.DB.ReserveBugRepair(t.Context(), fifth, bugCurrentRevision(t, f.DB, f.Session.ID))
	if err != nil {
		t.Fatalf("reserve ordinal 5: %v", err)
	}
	if reserved.Ordinal != 5 {
		t.Fatalf("ordinal = %d, want 5", reserved.Ordinal)
	}
}

package store

import (
	"errors"
	"testing"
)

// bugRepairCapFixture returns a session with an approved analysis and a
// running "attempt" graph attempt any repair reservation can cite.
func bugRepairCapFixture(t *testing.T, issue int) (*DB, Session, BugAnalysisRevision, BugAnalysisApproval, GraphNodeAttempt) {
	t.Helper()
	db := newTestDB(t)
	session, identity := bugFixtureSession(t, db, issue)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	analystAttempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))
	revision, approval := bugFixtureApprovedAnalysis(t, db, session, identity, instance, analystAttempt, run, bugFixtureTime(0))
	fixerAttempt := bugFixtureAttempt(t, db, instance, "attempt", 0, bugFixtureTime(0))
	return db, session, revision, approval, fixerAttempt
}

// TestReserveBugRepair_ThreeAttemptCap is AC-004: exactly three lifetime
// reservations succeed without a grant, and the count is enforced by counting
// bug_repairs rows in ReserveBugRepair rather than a database CHECK.
func TestReserveBugRepair_ThreeAttemptCap(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 410)

	causes := []string{"initial", "test_failure", "test_failure"}
	for i, cause := range causes {
		request := "repair-" + string(rune('1'+i))
		in := bugFixtureRepairRequest(session, revision, approval, request, cause, attempt.ID, "")
		reserved, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID))
		if err != nil {
			t.Fatalf("reserve ordinal %d: %v", i+1, err)
		}
		if reserved.Ordinal != i+1 || reserved.State != "reserved" {
			t.Fatalf("reserve ordinal %d: got %+v", i+1, reserved)
		}
		if err := db.StopBugRepair(t.Context(), session.ID, request, "failed", "project checks failed", true); err != nil {
			t.Fatalf("stop ordinal %d: %v", i+1, err)
		}
	}

	// A CHECK on the ordinal column would reject the row outright with a
	// distinct SQLite error; the cap must instead be ErrBugRepairExhausted,
	// proving it is enforced by ReserveBugRepair's own row count.
	fourth := bugFixtureRepairRequest(session, revision, approval, "repair-4", "test_failure", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), fourth, bugCurrentRevision(t, db, session.ID)); !errors.Is(err, ErrBugRepairExhausted) {
		t.Fatalf("fourth reservation without a grant = %v, want ErrBugRepairExhausted", err)
	}

	// The three reservations are the only rows; the cap did not silently drop
	// or overwrite an earlier one.
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM bug_repairs WHERE session_id=?`, session.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("bug_repairs holds %d rows, want 3", count)
	}
}

// TestReserveBugRepair_ReplayReturnsOriginalReservation pins that reserving
// the exact same request twice is idempotent rather than a second spend.
func TestReserveBugRepair_ReplayReturnsOriginalReservation(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 411)
	in := bugFixtureRepairRequest(session, revision, approval, "repair-1", "initial", attempt.ID, "")

	first, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID))
	if err != nil {
		t.Fatal(err)
	}
	// A stale expected revision must not matter for an exact replay.
	second, err := db.ReserveBugRepair(t.Context(), in, 999999)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second != first {
		t.Fatalf("replay = %+v, want identical %+v", second, first)
	}

	changed := in
	changed.Evidence = "a different story entirely"
	if _, err := db.ReserveBugRepair(t.Context(), changed, bugCurrentRevision(t, db, session.ID)); err == nil {
		t.Fatal("changed input for the same request was accepted as a replay")
	}
}

// TestReserveBugRepair_RequiresCurrentApproval pins that a repair citing a
// review the owner never approved (or approved for a different analysis
// revision/digest) is refused, independent of the attempt budget.
func TestReserveBugRepair_RequiresCurrentApproval(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 412)

	unapproved := approval
	unapproved.Review = "no-such-review"
	in := bugFixtureRepairRequest(session, revision, unapproved, "repair-1", "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); !errors.Is(err, ErrBugRepairUnauthorized) {
		t.Fatalf("unapproved review = %v, want ErrBugRepairUnauthorized", err)
	}

	staleDigest := revision
	staleDigest.Digest = "0000000000000000000000000000000000000000000000000000000000000000"[:64]
	bad := bugFixtureRepairRequest(session, staleDigest, approval, "repair-2", "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), bad, bugCurrentRevision(t, db, session.ID)); !errors.Is(err, ErrBugRepairUnauthorized) {
		t.Fatalf("mismatched digest = %v, want ErrBugRepairUnauthorized", err)
	}
}

// TestReserveBugRepair_InitialMustBeFirst pins that the "initial" cause is
// only accepted as the very first lifetime reservation.
func TestReserveBugRepair_InitialMustBeFirst(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 413)

	first := bugFixtureRepairRequest(session, revision, approval, "repair-1", "test_failure", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), first, bugCurrentRevision(t, db, session.ID)); err == nil {
		t.Fatal("a non-initial cause was accepted as the first reservation")
	}

	ok := bugFixtureRepairRequest(session, revision, approval, "repair-1", "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), ok, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}
	if err := db.StopBugRepair(t.Context(), session.ID, "repair-1", "failed", "still broken", true); err != nil {
		t.Fatal(err)
	}
	again := bugFixtureRepairRequest(session, revision, approval, "repair-2", "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), again, bugCurrentRevision(t, db, session.ID)); err == nil {
		t.Fatal("a second initial cause was accepted after the first reservation")
	}
}

// TestReserveBugRepair_GrantReferenceRulesWithinBudget pins that a grant
// reference is refused inside the base three-attempt budget (AC-006's other
// half from ReserveBugRepair's perspective).
func TestReserveBugRepair_GrantReferenceRulesWithinBudget(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 414)
	in := bugFixtureRepairRequest(session, revision, approval, "repair-1", "initial", attempt.ID, "exhaustion:1")
	if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); err == nil {
		t.Fatal("a grant reference was accepted within the base budget")
	}
}

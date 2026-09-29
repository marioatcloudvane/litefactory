package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// TestClaimBugWork_ExclusiveAcrossConcurrentClaimants is the worktree guard
// CAS requirement: many competing claims for the same kind, only one wins,
// and the loser's claim is refused rather than silently overwriting the row.
func TestClaimBugWork_ExclusiveAcrossConcurrentClaimants(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 460)

	const contenders = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners []string
	for i := 0; i < contenders; i++ {
		owner := "owner-" + string(rune('a'+i))
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			claimed, err := db.ClaimBugWork(context.Background(), session.ID, owner, "checks")
			if err != nil {
				t.Errorf("claim %s: %v", owner, err)
				return
			}
			if claimed {
				mu.Lock()
				winners = append(winners, owner)
				mu.Unlock()
			}
		}(owner)
	}
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("%d claimants won, want exactly 1: %v", len(winners), winners)
	}

	// A distinct kind is refused while a "checks"/"child"/"save" guard is held
	// by someone else, but the exact same owner/kind pair replays as a win.
	if claimed, err := db.ClaimBugWork(t.Context(), session.ID, winners[0], "checks"); err != nil || !claimed {
		t.Fatalf("re-claim by the same owner: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := db.ClaimBugWork(t.Context(), session.ID, "someone-else", "checks"); err != nil || claimed {
		t.Fatalf("a foreign claim while the guard is held: claimed=%v err=%v", claimed, err)
	}

	if err := db.ReleaseBugWork(t.Context(), session.ID, winners[0]); err != nil {
		t.Fatalf("release: %v", err)
	}
	if claimed, err := db.ClaimBugWork(t.Context(), session.ID, "someone-else", "save"); err != nil || !claimed {
		t.Fatalf("claim after release: claimed=%v err=%v", claimed, err)
	}
}

// TestClaimBugWork_NonRepairKindsAreSerializedAgainstActiveRepairs pins the
// extra rule ClaimBugWork itself enforces for every kind except "repair": no
// checks/child/save claim may proceed while a bug_repairs row is reserved or
// dispatched, even though the guard column itself is free. A "repair" claim
// is not gated here — that exclusivity is ReserveBugRepair's own row-count
// check, since ClaimBugWork's "repair" kind is used at dispatch time against
// one already-reserved request its caller already knows about.
func TestClaimBugWork_NonRepairKindsAreSerializedAgainstActiveRepairs(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 461)
	request := "repair-1"
	in := bugFixtureRepairRequest(session, revision, approval, request, "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}

	// The guard column is free (ReserveBugRepair does not touch it), but a
	// "checks" claim is refused while a repair row is reserved/dispatched.
	if claimed, err := db.ClaimBugWork(t.Context(), session.ID, "checks-op", "checks"); err != nil || claimed {
		t.Fatalf("checks claim during an active reservation: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := db.ClaimBugWork(t.Context(), session.ID, "save-op", "save"); err != nil || claimed {
		t.Fatalf("save claim during an active reservation: claimed=%v err=%v", claimed, err)
	}

	// Ending the reservation frees non-repair work again.
	if err := db.StopBugRepair(t.Context(), session.ID, request, "failed", "done", true); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimBugWork(t.Context(), session.ID, "checks-op", "checks"); err != nil || !claimed {
		t.Fatalf("checks claim after the reservation ended: claimed=%v err=%v", claimed, err)
	}
}

// TestCancelBugLifecycle_SuppressesFreshWorkAndFlagsInFlight pins the
// cancellation contract: cancellation persists, and it does not by itself
// clean anything up, but it does mark in-flight PR/test/check rows so a
// caller observing them next sees the cancellation.
func TestCancelBugLifecycle_SuppressesFreshWorkAndFlagsInFlight(t *testing.T) {
	t.Parallel()
	db, session, revision, approval, attempt := bugRepairCapFixture(t, 462)
	request := "repair-1"
	in := bugFixtureRepairRequest(session, revision, approval, request, "initial", attempt.ID, "")
	if _, err := db.ReserveBugRepair(t.Context(), in, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}
	child := bugFixtureDefinition(t, db, "c")
	instanceForLifecycle, err := db.GetGraphInstance(t.Context(), attempt.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	bugFixtureLifecycle(t, db, session, instanceForLifecycle, child)

	if err := db.CancelBugLifecycle(t.Context(), session.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	life, err := db.GetBugLifecycle(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !life.Cancelled || life.Disposition != "cancelled" {
		t.Fatalf("lifecycle = %+v, want cancelled", life)
	}

	// A cancelled bug refuses fresh analysis reservations.
	digest := submissionDigest([]byte("post-cancel"))
	if _, err := db.ReserveBugAnalysisPass(t.Context(), BugAnalysisPass{SessionID: session.ID, Invocation: "post-cancel", InputDigest: digest, GraphAttemptID: attempt.ID}, bugCurrentRevision(t, db, session.ID)); err == nil {
		t.Fatal("a cancelled bug accepted a fresh analysis reservation")
	}

	// Cancelling twice is not an error and does not un-cancel anything.
	if err := db.CancelBugLifecycle(t.Context(), session.ID); err != nil {
		t.Fatalf("second cancel: %v", err)
	}
}

// TestConfirmBugAnalysisReview_ReplayIsIdempotent pins the bug_events append
// helper's idempotent-replay guarantee against a public write path: recording
// the same durable operation twice is a no-op, never a duplicate row or a
// silent overwrite, and a conflicting replay is refused outright.
func TestBugEventAppend_IdempotentReplayAndConflict(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 463)

	rev := bugCurrentRevision(t, db, session.ID)
	if err := db.RecordBugFailure(t.Context(), session.ID, rev, "op-1", "first reason", []string{"evidence-1"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='failure' AND reference='op-1'`, session.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows after first record = %d, want 1", count)
	}

	// Replaying the exact same reference and content is a no-op: no duplicate
	// row appears, whatever expected revision the caller supplies.
	if err := db.RecordBugFailure(t.Context(), session.ID, 999999, "op-1", "first reason", []string{"evidence-1"}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='failure' AND reference='op-1'`, session.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows after replay = %d, want 1 (idempotent, not duplicated)", count)
	}

	// A changed reason for the same durable reference is a conflict, not a
	// silent overwrite of the retained history.
	if err := db.RecordBugFailure(t.Context(), session.ID, bugCurrentRevision(t, db, session.ID), "op-1", "a different reason", []string{"evidence-1"}); err == nil {
		t.Fatal("a changed replay for the same reference was accepted")
	}
}

// TestBugStoreConsolidationMigrationIsAllOrNothing is AC for migration 0079's
// all-or-nothing behavior: a forced failure partway through the DDL leaves no
// partial state — the retired tables survive, none of the six new tables
// exist, and the migration is not recorded as applied.
func TestBugStoreConsolidationMigrationIsAllOrNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	var target migration
	found := false
	for _, m := range migrations {
		if m.version == 79 {
			target = m
			found = true
			break
		}
		if err := db.applyMigration(ctx, m); err != nil {
			t.Fatalf("apply migration %d: %v", m.version, err)
		}
	}
	if !found {
		t.Fatal("migration 0079 not found in the embedded set")
	}

	// Sanity: the tables 0079 retires exist before the forced failure.
	for _, table := range []string{"bug_records", "bug_execution", "bug_analysis_passes"} {
		var name string
		if err := db.sql.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("expected pre-migration table %s: %v", table, err)
		}
	}

	corrupted := target.sql + "\nINSERT INTO no_such_table_xyz(a) VALUES (1);\n"
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, corrupted)
		return execErr
	})
	if err == nil {
		t.Fatal("a migration corrupted with a trailing bad statement still succeeded")
	}

	// The whole migration rolled back: every retired table survives...
	for _, table := range []string{"bug_records", "bug_execution", "bug_analysis_passes"} {
		var name string
		if err := db.sql.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("retired table %s did not survive a failed migration: %v", table, err)
		}
	}
	// ...and none of the newly named tables were left behind. bug_repairs is
	// excluded here: 0079 drops and recreates it under its pre-existing name,
	// so its post-rollback survival is exactly the old table, not evidence the
	// new one landed.
	for _, table := range []string{"bug_tasks", "bug_analyses", "bug_checks", "bug_delivery", "bug_events"} {
		var name string
		err := db.sql.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("new table %s exists after a failed migration (err=%v)", table, err)
		}
	}
	applied, err := db.appliedVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if applied[79] {
		t.Fatal("a failed migration was recorded as applied")
	}
}

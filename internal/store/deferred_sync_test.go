package store

import "testing"

func TestDeferredSyncRoundTripsDivergenceWithoutCommit(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 171)
	run := testRun(t, db, session.ID, 17101)
	if _, err := db.RecordRunSync(t.Context(), run.ID, RunSyncOutcome{Outcome: SyncDeferredDirty, Behind: 3, Ahead: 2}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncOutcome != SyncDeferredDirty || got.SyncBehind != 3 || got.SyncAhead != 2 || got.SyncCommitSHA != "" {
		t.Fatalf("round trip = %+v", got)
	}
	if _, err := db.RecordRunSync(t.Context(), run.ID, RunSyncOutcome{Outcome: SyncDeferredDirty, Behind: 3, Ahead: 2, CommitSHA: "not-a-sync"}); err == nil {
		t.Fatal("deferred sync accepted a commit SHA")
	}
	got, err = db.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncCommitSHA != "" || got.SyncOutcome != SyncDeferredDirty {
		t.Fatalf("rejected write mutated persistence: %+v", got)
	}
}

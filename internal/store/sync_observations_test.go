package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunSyncObservationsRoundTripAndHistoricalDefault(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 209)
	run := testRun(t, db, session.ID, 209)
	historical, err := db.RecordRunSync(t.Context(), run.ID, RunSyncOutcome{Outcome: SyncSynced, Behind: 2, Ahead: 1, CommitSHA: "historical-base-merge"})
	if err != nil {
		t.Fatal(err)
	}
	if len(historical.SyncObservations) != 0 {
		t.Fatal("historical record invents task observation")
	}
	for _, outcome := range []string{"absent", "up_to_date", "synced", "blocked", "skipped", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			want := []SyncObservation{{Phase: "task", Ref: "origin/lf/209-task", LocalSHA: "task-before", RemoteSHA: "operator-tip", Outcome: outcome, Behind: 2, Ahead: 1, ResultSHA: "task-result", Conflicts: []string{"file.go"}, Reason: "task reason"}, {Phase: "base", Ref: "origin/main", LocalSHA: "task-result", RemoteSHA: "base-tip", Outcome: "synced", Behind: 3, Ahead: 4, ResultSHA: "base-result", Conflicts: []string{}, Reason: "base reason"}}
			if outcome == "absent" {
				want[0].RemoteSHA = ""
			}
			got, err := db.RecordRunSyncObservations(t.Context(), run.ID, want)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.SyncObservations, want) {
				t.Fatalf("returned observations: %+v", got.SyncObservations)
			}
			loaded, err := db.GetRun(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(loaded.SyncObservations, want) || loaded.SyncCommitSHA != "historical-base-merge" {
				t.Fatalf("lost phase or legacy fields: %+v", loaded)
			}
		})
	}
}

func TestTaskSyncMigrationPreservesExistingBaseOnlyRun(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, 29)
	session := testSession(t, db, 209)
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "old-session", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.sql.ExecContext(t.Context(), `INSERT INTO runs (session_id,segment_id,ordinal,mode,agent,trigger_comment_id,status,started_at,log_path,sync_outcome,sync_behind,sync_ahead,sync_commit_sha) VALUES (?,?,1,?,?,0,?,?,?,'synced',2,1,'old-base-commit')`, session.ID, segment.ID, ModeSpike, AgentClaude, string(RunRunning), FormatTime(nowUTC()), "/logs/old.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	run, err := db.GetRun(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.SyncObservations) != 0 || run.SyncOutcome != SyncSynced || run.SyncBehind != 2 || run.SyncAhead != 1 || run.SyncCommitSHA != "old-base-commit" {
		t.Fatalf("migration altered historical evidence: %+v", run)
	}
}

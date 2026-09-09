package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func recoveryIntent(run Run) (RunOutcome, *Continuation) {
	now := run.StartedAt.Add(time.Second)
	zero := int64(0)
	return RunOutcome{SemanticOutcome: "interrupted", Status: RunFailed, ExitCode: &zero, FinishedAt: now}, &Continuation{ResetBound: now.Add(time.Hour), DueAt: now.Add(time.Hour + time.Minute), Precision: "exact", Context: ContinuationContext{Task: "github:acme/api#123", Worktree: "/worktree", Agent: run.Agent, Mode: run.Mode, Request: "implement original request", ResumeIdentity: "new-handle", SessionRef: "session-ref", SegmentOrdinal: 1, OwnerCommentID: run.TriggerCommentID, ApprovedGeneration: 1}}
}

func TestContinuationCompletionIsAtomicAndIdempotent(t *testing.T) {
	for _, trigger := range []int64{0, 42, scheduleTriggerBase + 99} {
		t.Run(time.Unix(trigger, 0).String(), func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 123)
			run := testRun(t, db, session.ID, trigger)
			outcome, intent := recoveryIntent(run)
			bad := *intent
			bad.Context.ResumeIdentity = ""
			if _, _, err := db.FinishInterruptedRun(t.Context(), run.ID, outcome, &bad, "new-handle"); err == nil {
				t.Fatal("invalid intent accepted")
			}
			stored, err := db.GetRun(t.Context(), run.ID)
			if err != nil || stored.Status != RunRunning {
				t.Fatalf("failed transaction changed run: %+v %v", stored, err)
			}
			segment, err := db.GetSegment(t.Context(), run.SegmentID)
			if err != nil || segment.ResumeToken != "" {
				t.Fatalf("failed transaction changed token: %+v %v", segment, err)
			}
			_, first, err := db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "new-handle")
			if err != nil {
				t.Fatal(err)
			}
			_, second, err := db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "different")
			if err != nil || first.ID != second.ID {
				t.Fatalf("replay: %+v %v", second, err)
			}
			pending, err := db.ListPendingContinuations(t.Context(), 0)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending = %+v %v", pending, err)
			}
			if pending[0].SourceRunID != run.ID || pending[0].RootRunID != run.ID || pending[0].Attempt != 1 || pending[0].Origin != "session_limit" || IsScheduleTrigger(first.TriggerID()) || !IsAutomaticTrigger(first.TriggerID()) {
				t.Fatalf("identities: %+v", pending[0])
			}
			if !reflect.DeepEqual(pending[0].Context, intent.Context) {
				t.Fatalf("snapshot mismatch: %+v", pending[0].Context)
			}
			segment, err = db.GetSegment(t.Context(), run.SegmentID)
			if err != nil || segment.ResumeToken != "new-handle" {
				t.Fatalf("token = %+v %v", segment, err)
			}
			reported, err := db.CompleteRunReport(t.Context(), run.ID, 100)
			if err != nil || reported.SemanticOutcome != "interrupted" || reported.Succeeded() {
				t.Fatalf("reported interruption = %+v %v", reported, err)
			}
			_, again, err := db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "different")
			if err != nil || again.ID != first.ID || again.AnnouncementCommentID != 100 {
				t.Fatalf("reported replay = %+v %v", again, err)
			}
		})
	}
}

func TestContinuationRestartPreservesIntentAndIndependentReportDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 42)
	outcome, intent := recoveryIntent(run)
	_, saved, err := db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "new-handle")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.GetContinuationBySource(t.Context(), run.ID)
	if err != nil || !reflect.DeepEqual(got, *saved) {
		t.Fatalf("restart snapshot: %+v %v", got, err)
	}
	pending, err := db.ListPendingInterruptionReports(t.Context())
	if err != nil || len(pending) != 1 || pending[0].ID != run.ID {
		t.Fatalf("report discovery: %+v %v", pending, err)
	}
	if _, err = db.CompleteRunReport(t.Context(), run.ID, 900); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListPendingInterruptionReports(t.Context())
	if err != nil || len(pending) != 0 {
		t.Fatalf("reported run still pending: %+v %v", pending, err)
	}
	actions, err := db.ListPendingContinuations(t.Context(), session.ID)
	if err != nil || len(actions) != 1 {
		t.Fatalf("report removed action: %+v %v", actions, err)
	}
}

func TestContinuationPreservesScheduledPrerequisiteAndManualUniqueness(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 123)
	due := time.Date(2026, 9, 9, 15, 0, 0, 0, time.FixedZone("local", 7200))
	params := CreateScheduleParams{SessionID: session.ID, SourceCommentID: 42, OwnerHandle: "owner", Payload: "original request", DueAt: due}
	schedule, created, err := db.CreateOrGetSchedule(t.Context(), params)
	if err != nil || !created {
		t.Fatalf("schedule: %+v %v", schedule, err)
	}
	run := testRun(t, db, session.ID, schedule.TriggerID())
	outcome, intent := recoveryIntent(run)
	intent.PrerequisiteID = schedule.ID
	_, saved, err := db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "handle")
	if err != nil {
		t.Fatal(err)
	}
	if saved.PrerequisiteID != schedule.ID || saved.DueAt.Location() != time.UTC {
		t.Fatalf("lost prerequisite or UTC: %+v", saved)
	}
	replay, created, err := db.CreateOrGetSchedule(t.Context(), params)
	if err != nil || created || replay.ID != schedule.ID {
		t.Fatalf("manual uniqueness changed: %+v %v", replay, err)
	}
	if string(replay.Status) != "suspended" {
		t.Fatalf("prerequisite not suspended: %+v", replay)
	}
}

package store

import (
	"context"
	"errors"
	"testing"
)

func TestPrelaunchAllocationAndExclusiveRestart(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 901)
	launched := testRun(t, db, session.ID, 1)
	pre, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: launched.SegmentID, Mode: ModeSpike, Agent: AgentClaude, TriggerCommentID: 2, Prelaunch: true, LogPath: func(int) string { return "/logs/pre.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	if pre.Status != RunPrelaunch || pre.Reportable() {
		t.Fatalf("prelaunch = %+v", pre)
	}
	if _, err := db.CompleteRunReport(t.Context(), pre.ID, 99); err == nil {
		t.Fatal("nonterminal report acknowledged")
	}
	if _, err := db.ReconcileOrphans(t.Context(), "restarted"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetRun(t.Context(), pre.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunPrelaunch {
		t.Fatalf("restart destroyed retryable preparation: %+v", got)
	}
	dead, err := db.GetRun(t.Context(), launched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != RunFailed || !dead.Reportable() {
		t.Fatalf("launched orphan = %+v", dead)
	}
	started, err := db.StartRun(t.Context(), pre.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != RunRunning {
		t.Fatalf("start = %+v", started)
	}
	if _, err := db.StartRun(t.Context(), pre.ID); err == nil {
		t.Fatal("running row relaunched")
	}
	if _, err := db.FinishRun(t.Context(), pre.ID, RunOutcome{Status: RunFailed, Error: "stopped"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartRun(t.Context(), pre.ID); err == nil {
		t.Fatal("terminal row reset")
	}
}

func TestReadinessRefusalRemainsTypedAndAtomic(t *testing.T) {
	db := newTestDB(t)
	parent := "github:acme/api#901"
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: parent, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckManagedReadiness(t.Context(), parent); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("early refusal: %v", err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), parent, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckManagedReadiness(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), parent, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), parent, 1, nil); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("raced refusal: %v", err)
	}
	f, err := db.GetManagedFeature(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if f.State != "planning" {
		t.Fatalf("raced refusal promoted scope: %+v", f)
	}
}

func TestPrelaunchScheduledAndAutomaticReservationsSurviveRestart(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "schedule", true: "automatic"}[automatic], func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 123)
			base := testRun(t, db, session.ID, 10)
			var trigger int64
			var action *Continuation
			var scheduled Schedule
			if automatic {
				outcome, intent := recoveryIntent(base)
				var err error
				_, action, err = db.FinishInterruptedRun(t.Context(), base.ID, outcome, intent, "new-handle")
				if err != nil {
					t.Fatal(err)
				}
				if _, ok, err := db.ReserveContinuation(t.Context(), action.ID, action.DueAt); err != nil || !ok {
					t.Fatalf("reserve=%v %v", ok, err)
				}
				trigger = action.TriggerID()
			} else {
				var err error
				scheduled, _, err = db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: session.ID, SourceCommentID: 20, OwnerHandle: "owner", Payload: "/spike task", DueAt: base.StartedAt})
				if err != nil {
					t.Fatal(err)
				}
				if _, ok, err := db.ReserveSchedule(t.Context(), scheduled.ID, base.StartedAt); err != nil || !ok {
					t.Fatalf("reserve=%v %v", ok, err)
				}
				trigger = scheduled.TriggerID()
			}
			pre, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: base.SegmentID, Mode: base.Mode, Agent: base.Agent, TriggerCommentID: trigger, Prelaunch: true, LogPath: func(int) string { return "/logs/pre.jsonl" }})
			if err != nil {
				t.Fatal(err)
			}
			path := db.path
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			*db = *reopened
			if _, err := db.ReconcileOrphans(t.Context(), "restarted"); err != nil {
				t.Fatal(err)
			}
			got, err := db.GetRun(t.Context(), pre.ID)
			if err != nil || got.Status != RunPrelaunch {
				t.Fatalf("prelaunch lost: %+v %v", got, err)
			}
			if automatic {
				c, err := db.GetContinuation(t.Context(), action.ID)
				if err != nil || c.Status != "pending" || c.AllocatedRunID != 0 {
					t.Fatalf("continuation lost: %+v %v", c, err)
				}
				if _, ok, err := db.ReserveContinuation(t.Context(), action.ID, action.DueAt); err != nil || !ok {
					t.Fatalf("re-reserve: %v %v", ok, err)
				}
			} else {
				s, err := db.GetSchedule(t.Context(), scheduled.ID)
				if err != nil || s.Status != SchedulePending {
					t.Fatalf("schedule lost: %+v %v", s, err)
				}
			}
			if _, err := db.StartRun(t.Context(), pre.ID); err != nil {
				t.Fatal(err)
			}
			runs, err := db.ListRuns(t.Context(), session.ID)
			if err != nil || len(runs) != 2 {
				t.Fatalf("duplicate allocation: %+v %v", runs, err)
			}
		})
	}
}

func TestCancelledSessionRunningWritePreservesPrelaunch(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 321)
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "preparation", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: ModeSpike, Agent: AgentClaude, TriggerCommentID: 123, Prelaunch: true, LogPath: func(int) string { return "/logs/pre.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.SetSessionState(ctx, session.ID, SessionRunning); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write=%v", err)
	}
	current, err := db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != session.State {
		t.Fatalf("failed write changed session: %+v", current)
	}
	currentRun, err := db.GetRun(t.Context(), run.ID)
	if err != nil || currentRun.Status != RunPrelaunch {
		t.Fatalf("cancelled write lost allocation: %+v %v", currentRun, err)
	}
	if err := db.SetSessionState(t.Context(), session.ID, SessionRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartRun(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessRefusesPendingRefinementWithReadyFlag(t *testing.T) {
	db, session, revisions := approvedFixture(t)
	pending := StoryRefinement{SessionID: session.ID, CommentID: 555, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}
	if err := db.AdmitStoryRefinement(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), session.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckManagedReadiness(t.Context(), session.TaskRef); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("pending early refusal=%v", err)
	}
	if err := db.ApproveManagedStories(t.Context(), session.TaskRef, 1, revisions); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("pending atomic refusal=%v", err)
	}
}

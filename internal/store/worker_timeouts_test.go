package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWorkerTimingSnapshotSurvivesReplay(t *testing.T) {
	db := newTestDB(t)
	for _, in := range []AttemptTiming{{NodeAttemptID: 1}, {RunID: 1}} {
		in.StartedAt = implementT0
		in.Deadline = implementT0.Add(91 * time.Second)
		in.Allowance = 91 * time.Second
		in.Source = "legacy"
		first, err := db.EnsureAttemptTiming(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		in.StartedAt = in.StartedAt.Add(time.Hour)
		in.Deadline = in.Deadline.Add(time.Hour)
		in.Allowance = time.Hour
		in.Source = "changed"
		replay, err := db.EnsureAttemptTiming(t.Context(), in)
		if err != nil || replay != first {
			t.Fatalf("snapshot changed: %+v %v", replay, err)
		}
	}
}

func heldStory(t *testing.T, db *DB) ImplementStory {
	t.Helper()
	runningStory(t, db, "a")
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", 77, implementT0); err != nil {
		t.Fatal(err)
	}
	row, err := db.HoldImplementStoryTimeout(t.Context(), 77, 11, "expired", implementT0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ConfirmImplementTimeoutNotice(t.Context(), row.Parent, row.Generation, row.Key, 11, "park-key", "100", implementT0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	row, err = db.GetImplementStory(t.Context(), row.Parent, row.Generation, row.Key)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestImplementTimeoutPendingReplyResumesExactlyOnce(t *testing.T) {
	db := newTestDB(t)
	row := heldStory(t, db)
	if row.Attempts != 0 || row.TimeoutParkRevision != row.Revision+1 {
		t.Fatalf("hold spent repairs or lost stable park: %+v", row)
	}
	workers, err := db.ListImplementStoriesNeedingWorker(t.Context())
	if err != nil || len(workers) != 0 {
		t.Fatalf("held worker runnable: %+v %v", workers, err)
	}
	reply := ImplementTimeoutReply{Source: "github", Task: row.Parent, CommentID: "101", AuthorID: "owner", OwnerID: "owner", Parent: row.Parent, Generation: 1, Key: "a", TimeoutAttemptID: 11, ParkRevision: row.TimeoutParkRevision, DeliveryKey: "park-key", NoticeCommentID: "100", Guidance: "continue", At: implementT0.Add(3 * time.Minute)}
	pending, resumed, err := db.ResumeImplementTimeout(t.Context(), reply)
	if err != nil || resumed || pending.State != ImplementRunning {
		t.Fatalf("early reply: %+v %t %v", pending, resumed, err)
	}
	parked, err := db.TransitionImplementStory(t.Context(), ImplementTransition{Parent: row.Parent, Generation: 1, Key: "a", From: ImplementRunning, To: ImplementParked, ExpectedRevision: row.Revision, Reason: "expired", At: implementT0.Add(4 * time.Minute)})
	if err != nil || parked.State != ImplementWaiting || parked.TimeoutAttemptID != 0 || parked.WorkerSession != 77 || parked.Guidance != "continue" {
		t.Fatalf("pending settlement: %+v %v", parked, err)
	}
	replay, resumed, err := db.ResumeImplementTimeout(t.Context(), reply)
	if err != nil || resumed || replay.Revision != parked.Revision {
		t.Fatalf("reply replay changed row: %+v %t %v", replay, resumed, err)
	}
	running, err := db.TransitionImplementStory(t.Context(), ImplementTransition{Parent: row.Parent, Generation: 1, Key: "a", From: ImplementWaiting, To: ImplementRunning, ExpectedRevision: parked.Revision, At: implementT0.Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetImplementWorkerSession(t.Context(), row.Parent, 1, "a", 77, implementT0.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	later, err := db.HoldImplementStoryTimeout(t.Context(), 77, 12, "expired again", implementT0.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	replay, resumed, err = db.ResumeImplementTimeout(t.Context(), reply)
	if err != nil || resumed || replay.TimeoutAttemptID != 12 || replay.Revision != running.Revision || later.TimeoutAttemptID != 12 {
		t.Fatalf("old reply cleared later hold: %+v %t %v", replay, resumed, err)
	}
}

func TestImplementTimeoutCannotBypassAuthenticatedResume(t *testing.T) {
	db := newTestDB(t)
	row := heldStory(t, db)
	row, err := db.TransitionImplementStory(t.Context(), ImplementTransition{Parent: row.Parent, Generation: 1, Key: "a", From: ImplementRunning, To: ImplementParked, ExpectedRevision: row.Revision, Reason: "expired", At: implementT0.Add(3 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.TransitionImplementStory(t.Context(), ImplementTransition{Parent: row.Parent, Generation: 1, Key: "a", From: ImplementParked, To: ImplementWaiting, ExpectedRevision: row.Revision, At: implementT0.Add(4 * time.Minute)})
	if !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("generic resume bypassed hold: %v", err)
	}
	reply := ImplementTimeoutReply{Source: "github", Task: row.Parent, CommentID: "101", AuthorID: "intruder", OwnerID: "owner", Parent: row.Parent, Generation: 1, Key: "a", TimeoutAttemptID: 11, ParkRevision: row.Revision, DeliveryKey: "park-key", NoticeCommentID: "100", At: implementT0.Add(4 * time.Minute)}
	if _, _, err = db.ResumeImplementTimeout(t.Context(), reply); err == nil {
		t.Fatal("unauthorized reply accepted")
	}
	reply.AuthorID = "owner"
	reply.DeliveryKey = "other"
	if _, _, err = db.ResumeImplementTimeout(t.Context(), reply); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("wrong notice accepted: %v", err)
	}
	reply.DeliveryKey = "park-key"
	reply.At = implementT0
	if _, _, err = db.ResumeImplementTimeout(t.Context(), reply); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("stale reply accepted: %v", err)
	}
}

func TestAttemptTimeoutAtomicallyHoldsItsImplementWorker(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	attempt := graphTestAttempt(t, db, instance, 3)
	runningStory(t, db, "a")
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", instance.SessionID, implementT0); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: 91 * time.Second, TimedOutAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a")
	if err != nil || row.TimeoutAttemptID != attempt.ID || row.Attempts != 0 {
		t.Fatalf("timeout did not hold worker: %+v %v", row, err)
	}
	if _, err = db.RecordImplementFailure(t.Context(), row.Parent, 1, row.Key, "ordinary failure", graphTestTime()); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("ordinary failure changed held attempt: %v", err)
	}
	// Receipt replay must never recreate the old hold after an authorized resume.
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE implement_stories SET timeout_attempt_id=0`); err != nil {
		t.Fatal(err)
	}
	if err = db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: time.Hour, TimedOutAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	row, err = db.GetImplementStory(t.Context(), row.Parent, 1, row.Key)
	if err != nil || row.TimeoutAttemptID != 0 {
		t.Fatalf("old timeout recreated hold: %+v %v", row, err)
	}
}

func TestAttemptTimeoutPreservesSubsecondParentAllowance(t *testing.T) {
	db := newTestDB(t)
	attempt := graphTestAttempt(t, db, graphTestInstance(t, db), 3)
	budget := 101*time.Millisecond + 3*time.Nanosecond
	if err := db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: budget, TimedOutAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetAttemptTimeout(t.Context(), attempt.ID)
	if err != nil || got.Budget != budget {
		t.Fatalf("parent allowance lost precision: %+v %v", got, err)
	}
}

func TestImplementTimeoutReplyUsesObservedRemoteNoticeTime(t *testing.T) {
	db := newTestDB(t)
	row := heldStory(t, db)
	// The remote comment exists before its local confirmation. A fast owner
	// reply in that gap must remain usable once publication is confirmed.
	reply := ImplementTimeoutReply{Source: "github", Task: row.Parent, CommentID: "101", AuthorID: "owner", OwnerID: "owner", Parent: row.Parent, Generation: 1, Key: "a", TimeoutAttemptID: 11, ParkRevision: row.TimeoutParkRevision, DeliveryKey: "park-key", NoticeCommentID: "100", NoticeCreatedAt: implementT0.Add(90 * time.Second), At: implementT0.Add(100 * time.Second)}
	_, resumed, err := db.ResumeImplementTimeout(t.Context(), reply)
	if err != nil || resumed {
		t.Fatalf("fast remote reply was lost: %t %v", resumed, err)
	}
	parked, err := db.TransitionImplementStory(t.Context(), ImplementTransition{Parent: row.Parent, Generation: 1, Key: "a", From: ImplementRunning, To: ImplementParked, ExpectedRevision: row.Revision, Reason: "expired", At: implementT0.Add(4 * time.Minute)})
	if err != nil || parked.State != ImplementWaiting {
		t.Fatalf("fast remote reply not applied: %+v %v", parked, err)
	}
}

func TestImplementTimeoutEqualTimestampNeedsObservedNoticeOrder(t *testing.T) {
	db := newTestDB(t)
	row := heldStory(t, db)
	reply := ImplementTimeoutReply{Source: "github", Task: row.Parent, CommentID: "101", AuthorID: "owner", OwnerID: "owner", Parent: row.Parent, Generation: 1, Key: "a", TimeoutAttemptID: 11, ParkRevision: row.TimeoutParkRevision, DeliveryKey: "park-key", NoticeCommentID: "100", NoticeCreatedAt: implementT0.Add(90 * time.Second), At: implementT0.Add(90 * time.Second)}
	if _, _, err := db.ResumeImplementTimeout(t.Context(), reply); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("unordered same-time reply accepted: %v", err)
	}
	reply.NoticePrecedesReply = true
	if _, resumed, err := db.ResumeImplementTimeout(t.Context(), reply); err != nil || resumed {
		t.Fatalf("observed ordered reply rejected: %t %v", resumed, err)
	}
}

func TestHeldRunningStoryDoesNotConsumeImplementCapacity(t *testing.T) {
	db := newTestDB(t)
	runningStory(t, db, "a", "b", "c", "d")
	seedImplement(t, db, "e")
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", 77, implementT0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.HoldImplementStoryTimeout(t.Context(), 77, 11, "expired", implementT0); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CountImplementRunning(t.Context(), "github:o/r#1", 1); err != nil || n != 3 {
		t.Fatalf("held story counted toward capacity: %d %v", n, err)
	}
	if _, err := move(t, db, "e", ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatalf("held story blocked waiting admission: %v", err)
	}
}

func TestRestartDowntimePreservesExpiredWorkerDeadline(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "expired", false: "historical-unknown"}[known], func(t *testing.T) {
			db := newTestDB(t)
			instance := graphTestInstance(t, db)
			run := testRun(t, db, instance.SessionID, 700)
			attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, RunID: run.ID, Limit: 3, StartedAt: graphTestTime()})
			if err != nil {
				t.Fatal(err)
			}
			runningStory(t, db, "a")
			if err = db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", instance.SessionID, graphTestTime()); err != nil {
				t.Fatal(err)
			}
			allowance := 91*time.Second + time.Nanosecond
			deadline := graphTestTime().Add(allowance)
			if !known {
				meter, err := db.StartMeterAttempt(t.Context(), run.ID, AgentClaude)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(t.Context(), `UPDATE meter_attempts SET graph_node_attempt_id=? WHERE id=?`, attempt.ID, meter.ID); err != nil {
					t.Fatal(err)
				}
			}
			if known {
				if _, err = db.EnsureAttemptTiming(t.Context(), AttemptTiming{NodeAttemptID: attempt.ID, StartedAt: graphTestTime(), Deadline: deadline, Allowance: allowance, Source: "legacy"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = db.ReconcileWorkflowGraphs(t.Context(), "implement", deadline.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			row, err := db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a")
			if err != nil {
				t.Fatal(err)
			}
			timeout, timeoutErr := db.GetAttemptTimeout(t.Context(), attempt.ID)
			if known {
				if timeoutErr != nil || timeout.Budget != allowance || !timeout.TimedOutAt.Equal(deadline.Truncate(time.Millisecond)) || row.TimeoutAttemptID != attempt.ID || row.Attempts != 0 {
					t.Fatalf("expired allowance reset: %+v %+v %v", row, timeout, timeoutErr)
				}
			} else if !errors.Is(timeoutErr, ErrNotFound) || row.State != ImplementParked || row.TimeoutAttemptID != 0 {
				t.Fatalf("unknown timing fabricated or runnable: %+v %+v %v", row, timeout, timeoutErr)
			}
			workers, err := db.ListImplementStoriesNeedingWorker(t.Context())
			if err != nil || len(workers) != 0 {
				t.Fatalf("restart permitted worker retry: %+v %v", workers, err)
			}
		})
	}
}

func TestRestartControlStepDoesNotInventHistoricalAgentTiming(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	run := testRun(t, db, instance.SessionID, 701)
	if _, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, RunID: run.ID, Limit: 3, StartedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	runningStory(t, db, "a")
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", instance.SessionID, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReconcileWorkflowGraphs(t.Context(), "implement", graphTestTime().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a")
	if err != nil || row.State != ImplementRunning || row.TimeoutAttemptID != 0 {
		t.Fatalf("control step treated as unknown provider: %+v %v", row, err)
	}
	workers, err := db.ListImplementStoriesNeedingWorker(t.Context())
	if err != nil || len(workers) != 1 {
		t.Fatalf("prior control recovery changed: %+v %v", workers, err)
	}
}

func TestOrphanNonGraphTimingDistinguishesExpiryAndCancellation(t *testing.T) {
	for _, kind := range []string{"expired", "live", "missing", "owner-stopped"} {
		t.Run(kind, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 710)
			run := testRun(t, db, session.ID, 711)
			reference := time.Now().UTC()
			deadline := reference.Add(-time.Minute)
			allowance := 91*time.Second + 3*time.Nanosecond
			if kind == "live" {
				deadline = reference.Add(time.Hour)
			}
			if kind != "missing" {
				if _, err := db.EnsureAttemptTiming(t.Context(), AttemptTiming{RunID: run.ID, StartedAt: deadline.Add(-allowance), Deadline: deadline, Allowance: allowance, Source: "legacy"}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "owner-stopped" {
				if _, err := db.RequestOwnerStop(t.Context(), session.TaskRef); err != nil {
					t.Fatal(err)
				}
			}
			result, err := db.ReconcileOrphans(t.Context(), "replacement")
			if err != nil || len(result.AbandonedRuns) != 1 {
				t.Fatalf("orphan reconciliation: %+v %v", result, err)
			}
			got := result.AbandonedRuns[0]
			switch kind {
			case "expired":
				if got.SemanticOutcome != "timed_out" || got.Error == ReasonAbandoned {
					t.Fatalf("expiry lost: %+v", got)
				}
			case "owner-stopped":
				if got.SemanticOutcome == "timed_out" || got.Error != ReasonOwnerStop {
					t.Fatalf("owner cancellation became expiry: %+v", got)
				}
			default:
				if got.SemanticOutcome == "timed_out" || got.Error != ReasonAbandoned {
					t.Fatalf("unexpired/unknown clock invented timeout: %+v", got)
				}
			}
		})
	}
}

func TestOtherWorkerTimeoutDoesNotHoldImplementStory(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 315)
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "core", Mode: "research", ProfileVersion: 1, Path: "core/research.dot", Digest: strings.Repeat("b", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "work", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	attempt := graphTestAttempt(t, db, instance, 3)
	runningStory(t, db, "a")
	if err = db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", session.ID, implementT0); err != nil {
		t.Fatal(err)
	}
	if err = db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: time.Minute, TimedOutAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a")
	if err != nil || row.TimeoutAttemptID != 0 || row.State != ImplementRunning {
		t.Fatalf("another worker acquired Implement hold: %+v %v", row, err)
	}
	if _, err = db.GetAttemptTimeout(t.Context(), attempt.ID); err != nil {
		t.Fatalf("other worker lost timeout evidence: %v", err)
	}
}

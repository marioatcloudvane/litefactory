package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// These reset bounds are synthetic future evidence. Provider syntax/provenance
// is covered by the adapter fixtures; this suite exercises durable behavior.
type recoveryChainFixture struct {
	db       *DB
	path     string
	session  Session
	original Schedule
	child    Schedule
	root     Run
	action   Continuation
}

func newRecoveryChainFixture(t *testing.T) *recoveryChainFixture {
	t.Helper()
	f := &recoveryChainFixture{path: filepath.Join(t.TempDir(), "recovery.db")}
	var err error
	f.db, err = Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := f.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.session = testSession(t, f.db, 160)
	now := time.Now().UTC().Truncate(time.Second)
	f.original, _, err = f.db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: f.session.ID, SourceCommentID: 20, OwnerHandle: "owner", Payload: "original", DueAt: now})
	if err != nil {
		t.Fatal(err)
	}
	f.child, _, err = f.db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: f.session.ID, SourceCommentID: 21, OwnerHandle: "owner", Payload: "dependent", Delay: time.Minute, PrerequisiteIDs: []int64{f.original.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.db.ReserveSchedule(t.Context(), f.original.ID, now); err != nil || !ok {
		t.Fatalf("reserve source=%v %v", ok, err)
	}
	f.root = testRun(t, f.db, f.session.ID, f.original.TriggerID())
	outcome, intent := recoveryIntent(f.root)
	intent.PrerequisiteID = f.original.ID
	_, c, err := f.db.FinishInterruptedRun(t.Context(), f.root.ID, outcome, intent, "saved-handle")
	if err != nil {
		t.Fatal(err)
	}
	f.action = *c
	return f
}

func (f *recoveryChainFixture) restart(t *testing.T) {
	t.Helper()
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.db, err = Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ReconcileOrphans(t.Context(), "replacement-daemon"); err != nil {
		t.Fatal(err)
	}
}

func (f *recoveryChainFixture) allocate(t *testing.T, c Continuation) Run {
	t.Helper()
	if _, ok, err := f.db.ReserveContinuation(t.Context(), c.ID, c.DueAt.Add(time.Hour)); err != nil || !ok {
		t.Fatalf("reserve overdue=%v %v", ok, err)
	}
	run, err := f.db.AllocateRun(t.Context(), AllocateRunParams{SessionID: c.SessionID, SegmentID: c.SegmentID, Mode: c.Context.Mode, Agent: c.Context.Agent, TriggerCommentID: c.TriggerID(), LogPath: func(n int) string { return fmt.Sprintf("/logs/%d.jsonl", n) }})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *recoveryChainFixture) assertSchedule(t *testing.T, status ScheduleStatus, childStatus ScheduleStatus) Schedule {
	t.Helper()
	s, err := f.db.GetSchedule(t.Context(), f.original.ID)
	if err != nil || s.Status != status {
		t.Fatalf("original=%+v err=%v want=%s", s, err, status)
	}
	child, err := f.db.GetSchedule(t.Context(), f.child.ID)
	if err != nil || child.Status != childStatus {
		t.Fatalf("child=%+v err=%v want=%s", child, err, childStatus)
	}
	if childStatus == SchedulePending && status != "succeeded" && !child.DueAt.IsZero() {
		t.Fatalf("dependent released before semantic success: %+v", child)
	}
	return s
}

func TestRecoveryChainRestartFaultMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		allocate bool
		reserve  bool
		verified bool
	}{
		{name: "pending intent survives report loss"},
		{name: "reservation without run requeues", reserve: true},
		{name: "allocated abandoned run never requeues", allocate: true},
		{name: "verified quota retains next intent", allocate: true, verified: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecoveryChainFixture(t)
			var allocated Run
			nextID := f.action.ID
			if tc.allocate {
				allocated = f.allocate(t, f.action)
			} else if tc.reserve {
				if _, ok, err := f.db.ReserveContinuation(t.Context(), f.action.ID, f.action.DueAt); err != nil || !ok {
					t.Fatalf("reserve=%v %v", ok, err)
				}
			}
			if tc.verified {
				outcome, intent := recoveryIntent(allocated)
				_, next, err := f.db.FinishInterruptedRun(t.Context(), allocated.ID, outcome, intent, "saved-handle")
				if err != nil {
					t.Fatal(err)
				}
				nextID = next.ID
			}
			f.restart(t)
			got, err := f.db.GetContinuation(t.Context(), f.action.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "pending"
			if tc.allocate {
				want = "failed"
			}
			if tc.verified {
				want = "interrupted"
			}
			if got.Status != want {
				t.Fatalf("restart action=%+v want=%s", got, want)
			}
			runs, err := f.db.ListRuns(t.Context(), f.session.ID)
			wantRuns := 1
			if tc.allocate {
				wantRuns = 2
			}
			if err != nil || len(runs) != wantRuns {
				t.Fatalf("reconciliation allocated work=%+v %v", runs, err)
			}
			pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.allocate && !tc.verified {
				if len(pending) != 0 {
					t.Fatalf("abandoned action requeued=%+v", pending)
				}
				f.assertSchedule(t, ScheduleFailed, ScheduleBlocked)
			} else {
				if len(pending) != 1 || pending[0].ID != nextID {
					t.Fatalf("recoverable action lost=%+v", pending)
				}
				f.assertSchedule(t, "suspended", SchedulePending)
			}
			reports, err := f.db.ListPendingInterruptionReports(t.Context())
			if err != nil || len(reports) == 0 || reports[0].ID != f.root.ID {
				t.Fatalf("source report debt lost=%+v %v", reports, err)
			}
			if _, err := f.db.CompleteRunReport(t.Context(), f.root.ID, 900); err != nil {
				t.Fatal(err)
			}
			source, err := f.db.GetRun(t.Context(), f.root.ID)
			if err != nil || source.Succeeded() || source.SemanticOutcome != "interrupted" {
				t.Fatalf("reported source changed semantics=%+v %v", source, err)
			}
			f.restart(t)
			again, err := f.db.ListRuns(t.Context(), f.session.ID)
			if err != nil || len(again) != wantRuns {
				t.Fatalf("second restart duplicates allocation=%+v %v", again, err)
			}
		})
	}
}

func TestRecoveryChainThreeActualInvocations(t *testing.T) {
	f := newRecoveryChainFixture(t)
	c := f.action
	for attempt := 1; attempt <= 3; attempt++ {
		// Reservations/reports and their retries must not count as invocations.
		if _, ok, err := f.db.ReserveContinuation(t.Context(), c.ID, c.DueAt); err != nil || !ok {
			t.Fatalf("reserve=%v %v", ok, err)
		}
		if err := f.db.RequeueContinuation(t.Context(), c.ID); err != nil {
			t.Fatal(err)
		}
		run := f.allocate(t, c)
		if c.Attempt != attempt || c.RootRunID != f.root.ID {
			t.Fatalf("chain identity=%+v want attempt=%d", c, attempt)
		}
		outcome, intent := recoveryIntent(run)
		if attempt == 3 {
			if _, _, err := f.db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "saved-handle"); err == nil {
				t.Fatal("fourth continuation accepted beyond root budget")
			}
			intent = nil
			outcome.RecoveryReason = "The three automatic invocation limit is exhausted; manual recovery is required."
		}
		_, next, err := f.db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "saved-handle")
		if err != nil {
			t.Fatal(err)
		}
		_, replayed, err := f.db.FinishInterruptedRun(t.Context(), run.ID, outcome, intent, "saved-handle")
		if err != nil {
			t.Fatal(err)
		}
		if (next == nil) != (replayed == nil) || (next != nil && next.ID != replayed.ID) {
			t.Fatalf("duplicate completion changed chain=%+v %+v", next, replayed)
		}
		if _, err := f.db.CompleteRunReport(t.Context(), run.ID, int64(1000+attempt)); err != nil {
			t.Fatal(err)
		}
		f.restart(t)
		if attempt < 3 {
			if next == nil {
				t.Fatal("fresh verified interruption lost continuation")
			}
			c = *next
			f.assertSchedule(t, "suspended", SchedulePending)
		} else {
			if next != nil {
				t.Fatalf("fourth automatic invocation scheduled=%+v", next)
			}
			f.assertSchedule(t, ScheduleFailed, ScheduleBlocked)
		}
	}
	pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("exhausted chain pending=%+v %v", pending, err)
	}
	runs, err := f.db.ListRuns(t.Context(), f.session.ID)
	if err != nil || len(runs) != 4 {
		t.Fatalf("actual invocations=%d want root+3: %v", len(runs), err)
	}
	// Even attempting to reserve an already consumed action cannot allocate again.
	if _, ok, err := f.db.ReserveContinuation(t.Context(), c.ID, c.DueAt.Add(time.Hour)); err != nil || ok {
		t.Fatalf("spent action admitted=%v %v", ok, err)
	}
}

func TestRecoveryChainTerminalSettlement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		success bool
		cancel  bool
		quota   bool
	}{
		{name: "semantic success", success: true}, {name: "failure"}, {name: "cancelled", cancel: true}, {name: "exit zero quota without reset or identity", quota: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecoveryChainFixture(t)
			finalAt := f.action.DueAt.Add(2 * time.Hour)
			if tc.cancel {
				if err := f.db.CancelContinuation(t.Context(), f.action.ID, "manual recovery required", finalAt); err != nil {
					t.Fatal(err)
				}
			} else {
				run := f.allocate(t, f.action)
				zero := int64(0)
				outcome := RunOutcome{Status: RunFailed, SemanticOutcome: "failed", ExitCode: &zero, FinishedAt: finalAt}
				if tc.success {
					outcome.Status = RunSucceeded
					outcome.SemanticOutcome = "succeeded"
				}
				if tc.quota {
					outcome.SemanticOutcome = "interrupted"
					outcome.RecoveryReason = "No usable reset or conversation; manual recovery required."
					if _, _, err := f.db.FinishInterruptedRun(t.Context(), run.ID, outcome, nil, ""); err != nil {
						t.Fatal(err)
					}
				} else if _, err := f.db.FinishRun(t.Context(), run.ID, outcome); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.CompleteRunReport(t.Context(), run.ID, 901); err != nil {
					t.Fatal(err)
				}
			}
			f.restart(t)
			if tc.success {
				s := f.assertSchedule(t, ScheduleSucceeded, SchedulePending)
				child, err := f.db.GetSchedule(t.Context(), f.child.ID)
				if err != nil || !s.FinishedAt.Equal(finalAt) || !child.DueAt.Equal(finalAt.Add(time.Minute)) {
					t.Fatalf("final release time original=%+v child=%+v err=%v", s, child, err)
				}
			} else {
				f.assertSchedule(t, ScheduleFailed, ScheduleBlocked)
			}
		})
	}
}

func TestRecoveryChainMultipleOriginalPrerequisites(t *testing.T) {
	f := newRecoveryChainFixture(t)
	other, _, err := f.db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: f.session.ID, SourceCommentID: 50, OwnerHandle: "owner", Payload: "other prerequisite", DueAt: f.action.DueAt})
	if err != nil {
		t.Fatal(err)
	}
	dependent, _, err := f.db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: f.session.ID, SourceCommentID: 51, OwnerHandle: "owner", Payload: "both complete", Delay: 2 * time.Minute, PrerequisiteIDs: []int64{f.original.ID, other.ID}})
	if err != nil {
		t.Fatal(err)
	}
	run := f.allocate(t, f.action)
	finalAt := f.action.DueAt.Add(time.Hour)
	zero := int64(0)
	if _, err := f.db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, SemanticOutcome: "succeeded", ExitCode: &zero, FinishedAt: finalAt}); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	waiting, err := f.db.GetSchedule(t.Context(), dependent.ID)
	if err != nil || !waiting.DueAt.IsZero() || waiting.Status != SchedulePending {
		t.Fatalf("released before second prerequisite=%+v %v", waiting, err)
	}
	if _, ok, err := f.db.ReserveSchedule(t.Context(), other.ID, finalAt); err != nil || !ok {
		t.Fatalf("reserve other=%v %v", ok, err)
	}
	secondAt := finalAt.Add(time.Hour)
	if _, err := f.db.FinishSchedule(t.Context(), other.ID, ScheduleSucceeded, 0, secondAt); err != nil {
		t.Fatal(err)
	}
	released, err := f.db.GetSchedule(t.Context(), dependent.ID)
	if err != nil || !released.DueAt.Equal(secondAt.Add(2*time.Minute)) {
		t.Fatalf("final prerequisite release=%+v %v", released, err)
	}
}

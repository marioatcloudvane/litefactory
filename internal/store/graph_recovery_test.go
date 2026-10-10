package store

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func closureWaitFixture(t *testing.T, mode, state string) (*DB, GraphInstance, GraphNodeAttempt) {
	t.Helper()
	db := newTestDB(t)
	session := testSession(t, db, 540)
	def := GraphDefinition{Team: "closure", Mode: mode, ProfileVersion: 1, Path: mode + ".dot", Digest: strings.Repeat("c", 64), CreatedAt: graphTestTime()}
	if mode == "bug" {
		def.Mode, def.Workflow = "", mode
	}
	def, err := db.CreateGraphDefinition(t.Context(), def)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "waiting", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	attempt := graphTestAttempt(t, db, instance, 3)
	if err = db.FinishGraphNodeAttempt(t.Context(), attempt.ID, GraphSucceeded, "done", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if err = db.RetainGraphComment(t.Context(), GraphCommentIntent{Key: "question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "waiting", Kind: "question", Candidate: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), "UPDATE graph_owner_waits SET state=?", state); err != nil {
		t.Fatal(err)
	}
	return db, instance, attempt
}

func TestClosedIssueOwnerWaitCancellation(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike", "bug"} {
		for _, state := range []string{"waiting", "claimed"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				db, instance, attempt := closureWaitFixture(t, mode, state)
				ctx := t.Context()
				dispatch, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, graphTestTime().Add(time.Hour), graphTestTime())
				if err != nil {
					t.Fatal(err)
				}
				if state == "claimed" {
					if _, err = db.sql.ExecContext(ctx, "UPDATE graph_dispatches SET status='reserved' WHERE id=?", dispatch.ID); err != nil {
						t.Fatal(err)
					}
				}
				before, err := db.GetGraphNodeAttempt(ctx, attempt.ID)
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err = db.CancelClosedIssueGraphs(ctx, instance.SessionID, graphTestTime().Add(time.Minute)); err != nil {
						t.Fatal(err)
					}
				}
				got, err := db.GetGraphInstance(ctx, instance.ID)
				if err != nil || got.Status != GraphCancelled || got.FinishedAt.IsZero() || !got.NextDueAt.IsZero() {
					t.Fatalf("instance=%+v err=%v", got, err)
				}
				wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
				if err != nil || wait.State != "cancelled" {
					t.Fatalf("wait=%+v err=%v", wait, err)
				}
				trace, err := db.ListGraphTransitions(ctx, instance.ID)
				if err != nil || len(trace) != 1 || trace[0].OnOutcome != "issue closed by owner" || trace[0].FromNode != instance.CurrentNode || trace[0].ToNode != instance.CurrentNode {
					t.Fatalf("trace=%+v err=%v", trace, err)
				}
				dispatch, err = scanGraphDispatch(db.sql.QueryRowContext(ctx, "SELECT "+graphDispatchColumns+" FROM graph_dispatches WHERE id=?", dispatch.ID))
				if err != nil || dispatch.Status != "cancelled" || dispatch.CancellationReason != "issue closed by owner" || dispatch.FinishedAt.IsZero() {
					t.Fatalf("dispatch=%+v err=%v", dispatch, err)
				}
				after, err := db.GetGraphNodeAttempt(ctx, attempt.ID)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("attempt changed: %+v %v", after, err)
				}
			})
		}
	}
}

func TestClosedIssueOwnerWaitRollback(t *testing.T) {
	db, instance, _ := closureWaitFixture(t, "validate", "waiting")
	ctx := t.Context()
	dispatch, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, graphTestTime(), graphTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, "CREATE TRIGGER reject_closure BEFORE INSERT ON graph_transitions BEGIN SELECT RAISE(ABORT,'injected trace failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = db.CancelClosedIssueGraphs(ctx, instance.SessionID, graphTestTime()); err == nil {
		t.Fatal("expected failure")
	}
	got, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil || got.Status != GraphRunning || got.NextDueAt.IsZero() {
		t.Fatalf("partial instance write: %+v %v", got, err)
	}
	wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("partial wait write: %+v %v", wait, err)
	}
	dispatch, err = scanGraphDispatch(db.sql.QueryRowContext(ctx, "SELECT "+graphDispatchColumns+" FROM graph_dispatches WHERE id=?", dispatch.ID))
	if err != nil || dispatch.Status != "pending" || dispatch.CancellationReason != "" {
		t.Fatalf("partial dispatch write: %+v %v", dispatch, err)
	}
}

func TestClosedIssueOwnerWaitProtectedAttemptsAndHistory(t *testing.T) {
	for _, mode := range []string{"research", "spike", "validate", "bug"} {
		for _, state := range []string{"pending", "running", "terminal", "allocated"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				db, instance, attempt := closureWaitFixture(t, mode, "claimed")
				ctx := t.Context()
				switch state {
				case "pending", "running":
					if _, err := db.sql.ExecContext(ctx, "UPDATE graph_node_attempts SET status=?,finished_at=NULL,outcome=NULL WHERE id=?", state, attempt.ID); err != nil {
						t.Fatal(err)
					}
				case "terminal":
					if err := db.SetGraphInstanceStatus(ctx, instance.ID, GraphFailed, time.Time{}, graphTestTime()); err != nil {
						t.Fatal(err)
					}
				case "allocated":
					if _, err := db.sql.ExecContext(ctx, "UPDATE graph_owner_waits SET state='allocated'"); err != nil {
						t.Fatal(err)
					}
				}
				before, err := db.GetGraphInstance(ctx, instance.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err = db.CancelClosedIssueGraphs(ctx, instance.SessionID, graphTestTime()); err != nil {
					t.Fatal(err)
				}
				after, err := db.GetGraphInstance(ctx, instance.ID)
				// Research and Spike retain their pre-existing non-wait cleanup.
				if state != "allocated" || (mode != "research" && mode != "spike") {
					if err != nil || !reflect.DeepEqual(before, after) {
						t.Fatalf("protected instance changed: %+v %v", after, err)
					}
				}
				wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
				want := "claimed"
				if state == "allocated" {
					want = state
				}
				if err != nil || wait.State != want {
					t.Fatalf("protected wait changed: %+v %v", wait, err)
				}
			})
		}
	}
}

func TestClosedIssueRunlessOwnerWait(t *testing.T) {
	db, _, instance, session := persistentBugStopFixture(t)
	if err := db.ParkGraphNode(t.Context(), GraphStop{Key: "runless", InstanceID: instance.ID, Origin: instance.CurrentNode, Cause: "refused", Reason: "no run", Candidate: []byte("{}"), At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if err := db.CancelClosedIssueGraphs(t.Context(), session.ID, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	trace, err := db.ListGraphTransitions(t.Context(), instance.ID)
	if err != nil || len(trace) != 1 || trace[0].OnOutcome != "issue closed by owner" {
		t.Fatalf("trace=%+v %v", trace, err)
	}
}

func TestClosedIssueOwnerWaitProtectsRunsAndAllocatedDispatch(t *testing.T) {
	for _, state := range []string{"prelaunch", "running", "allocated_dispatch"} {
		t.Run(state, func(t *testing.T) {
			db, instance, _ := closureWaitFixture(t, "research", "claimed")
			ctx := t.Context()
			segment, err := db.GetOrCreateSegment(ctx, instance.SessionID, 1, "research", AgentClaude)
			if err != nil {
				t.Fatal(err)
			}
			run, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: instance.SessionID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: 45, Prelaunch: state == "prelaunch", LogPath: func(int) string { return "closure.log" }})
			if err != nil {
				t.Fatal(err)
			}
			var dispatch GraphDispatch
			if state == "allocated_dispatch" {
				if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded, FinishedAt: run.StartedAt.Add(time.Second)}); err != nil {
					t.Fatal(err)
				}
				dispatch, err = db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, graphTestTime(), graphTestTime())
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(ctx, "UPDATE graph_dispatches SET status='running',allocated_run_id=? WHERE id=?", run.ID, dispatch.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := db.GetGraphInstance(ctx, instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.CancelClosedIssueGraphs(ctx, instance.SessionID, graphTestTime()); err != nil {
				t.Fatal(err)
			}
			after, err := db.GetGraphInstance(ctx, instance.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("protected graph changed: %+v %v", after, err)
			}
			if state == "allocated_dispatch" {
				if _, err = db.sql.ExecContext(ctx, "UPDATE graph_dispatches SET status='finished' WHERE id=?", dispatch.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded, FinishedAt: run.StartedAt.Add(time.Second)}); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.CancelClosedIssueGraphs(ctx, instance.SessionID, graphTestTime()); err != nil {
				t.Fatal(err)
			}
			after, err = db.GetGraphInstance(ctx, instance.ID)
			if err != nil || after.Status != GraphCancelled {
				t.Fatalf("settled work not cancelled: %+v %v", after, err)
			}
		})
	}
}

func TestClosedIssueOwnerWaitDoctorHold(t *testing.T) {
	db, instance, _ := closureWaitFixture(t, "validate", "waiting")
	session, err := db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, presentation := retainedPollProposal(t, db, session)
	if _, err = db.ReserveDoctorRepair(t.Context(), DoctorApproval{ID: "closure-hold", SourceID: "closure-approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation}); err != nil {
		t.Fatal(err)
	}
	if err = db.CancelClosedIssueGraphs(t.Context(), instance.SessionID, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || !reflect.DeepEqual(instance, got) {
		t.Fatalf("held graph changed: %+v %v", got, err)
	}
}

// #577: an idle wait is cancelled when it is open, or when it is claimed by
// exactly the named reply; any other claim keeps the instance running.
func TestCancelIdleGraphOwnerWaitAcceptsOnlyTheNamedClaim(t *testing.T) {
	for _, tc := range []struct {
		name         string
		state        string
		claimedReply int64
		want         bool
	}{
		{"open wait", "waiting", 0, true},
		{"open wait with a named reply", "waiting", 77, true},
		{"claimed without a named reply", "claimed", 0, false},
		{"claimed by another reply", "claimed", 78, false},
		{"claimed by the named reply", "claimed", 77, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, instance, _ := closureWaitFixture(t, "validate", tc.state)
			ctx := t.Context()
			if tc.state == "claimed" {
				if _, err := db.sql.ExecContext(ctx, "UPDATE graph_owner_waits SET reply_id=77 WHERE instance_id=?", instance.ID); err != nil {
					t.Fatal(err)
				}
			}
			got, err := db.CancelIdleGraphOwnerWait(ctx, instance.ID, tc.claimedReply, "the owner started Validate again", graphTestTime().Add(time.Minute))
			if err != nil || got != tc.want {
				t.Fatalf("cancelled=%v err=%v, want %v", got, err, tc.want)
			}
			inst, err := db.GetGraphInstance(ctx, instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want && (inst.Status != GraphCancelled || wait.State != "cancelled") {
				t.Fatalf("instance %s wait %s, want both cancelled", inst.Status, wait.State)
			}
			if !tc.want && (inst.Status != GraphRunning || wait.State != tc.state) {
				t.Fatalf("instance %s wait %s, want unchanged", inst.Status, wait.State)
			}
		})
	}
}

// A restart during a worker launch can leave an attempt open on an instance
// that already failed. Startup recovery cancels it with a reason; an attempt of
// a still-running instance keeps failing itself and its instance.
func TestReconcileWorkflowGraphsCancelsAttemptsOfFinishedInstances(t *testing.T) {
	db := newTestDB(t)
	def, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	open := func(issue int) (GraphInstance, GraphNodeAttempt) {
		session := testSession(t, db, issue)
		instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "budget", StartedAt: graphTestTime()})
		if err != nil {
			t.Fatal(err)
		}
		return instance, graphTestAttempt(t, db, instance, 3)
	}
	failed, orphan := open(587)
	if err = db.SetGraphInstanceStatus(t.Context(), failed.ID, GraphFailed, time.Time{}, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	running, interrupted := open(588)
	at := graphTestTime().Add(time.Hour)
	count, err := db.ReconcileWorkflowGraphs(t.Context(), "implement", at)
	if err != nil || count != 2 {
		t.Fatalf("reconciled %d: %v", count, err)
	}
	for _, c := range []struct {
		attempt               GraphNodeAttempt
		instance              int64
		attemptWant, instWant string
	}{{orphan, failed.ID, GraphCancelled, GraphFailed}, {interrupted, running.ID, GraphFailed, GraphFailed}} {
		got, err := db.GetGraphNodeAttempt(t.Context(), c.attempt.ID)
		if err != nil || got.Status != c.attemptWant || got.FinishedAt.IsZero() {
			t.Fatalf("attempt %d: %+v %v, want %s", c.attempt.ID, got, err, c.attemptWant)
		}
		outputs, err := db.ListGraphOutputs(t.Context(), c.attempt.ID)
		if err != nil || len(outputs) != 1 || outputs[0].Kind != "diagnostic" || len(outputs[0].Payload) == 0 {
			t.Fatalf("attempt %d lacks its reason: %+v %v", c.attempt.ID, outputs, err)
		}
		instance, err := db.GetGraphInstance(t.Context(), c.instance)
		if err != nil || instance.Status != c.instWant {
			t.Fatalf("instance %d: %+v %v", c.instance, instance, err)
		}
	}
	var left int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM graph_node_attempts WHERE status IN ('pending','running')`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d attempts still open: %v", left, err)
	}
}

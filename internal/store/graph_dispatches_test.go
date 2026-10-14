package store

import (
	"errors"
	"testing"
	"time"
)

func graphDispatchFixture(t *testing.T) (*DB, GraphInstance) {
	t.Helper()
	db := newTestDB(t)
	return db, graphTestInstance(t, db)
}

// The three private trigger bands must stay disjoint and ordered: a graph stage,
// an automatic continuation and a manual schedule all mint run identities, and
// one band leaking into another would let two of them collide on the runs
// table's (session, trigger) uniqueness.
func TestGraphTriggerBandIsDisjoint(t *testing.T) {
	t.Parallel()
	d := GraphDispatch{ID: 7}
	id := d.TriggerID()
	if !IsGraphTrigger(id) {
		t.Fatalf("%d is not recognised as a graph trigger", id)
	}
	if IsAutomaticTrigger(id) || IsScheduleTrigger(id) {
		t.Error("a graph trigger must not be read as recovery or schedule work")
	}
	for _, foreign := range []int64{0, 1, 9001, Continuation{ID: 7}.TriggerID(), Schedule{ID: 7}.TriggerID()} {
		if IsGraphTrigger(foreign) {
			t.Errorf("%d must not be read as a graph trigger", foreign)
		}
	}
}

func TestGraphDispatchLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, instance := graphDispatchFixture(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	parked, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, now, now)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if parked.Status != "pending" || parked.Node != instance.CurrentNode {
		t.Fatalf("unexpected dispatch: %+v", parked)
	}

	// Parking twice is how a retried turn behaves. It must not queue a second
	// stage; the open dispatch already describes the same intent.
	again, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, now, now)
	if err != nil {
		t.Fatalf("second park: %v", err)
	}
	if again.ID != parked.ID {
		t.Fatalf("a second park queued another dispatch: %d then %d", parked.ID, again.ID)
	}

	due, err := db.ListDueGraphDispatches(ctx, now, 10)
	if err != nil || len(due) != 1 || due[0].ID != parked.ID {
		t.Fatalf("due selection: %+v err=%v", due, err)
	}
	if early, err := db.ListDueGraphDispatches(ctx, now.Add(-time.Minute), 10); err != nil || len(early) != 0 {
		t.Fatalf("a dispatch must not be due before its time: %+v err=%v", early, err)
	}

	// Exactly one caller may claim it; that is what keeps two ticks from
	// starting the same stage twice.
	first, ok, err := db.ReserveGraphDispatch(ctx, parked.ID, now)
	if err != nil || !ok || first.Status != "reserved" {
		t.Fatalf("first reserve: ok=%v err=%v %+v", ok, err, first)
	}
	if _, ok, err := db.ReserveGraphDispatch(ctx, parked.ID, now); err != nil || ok {
		t.Fatalf("a reserved dispatch was claimed twice: ok=%v err=%v", ok, err)
	}

	// Losing a worker slot must not cost the instance its continuation, but the
	// retry is spaced out rather than taken on the very next tick.
	retry := now.Add(time.Minute)
	requeued, err := db.RequeueGraphDispatch(ctx, parked.ID, retry, "worker slot lost")
	if err != nil || !requeued {
		t.Fatalf("requeue: requeued=%v err=%v", requeued, err)
	}
	if due, err := db.ListDueGraphDispatches(ctx, now, 10); err != nil || len(due) != 0 {
		t.Fatalf("a backed-off dispatch must not be due yet: %+v err=%v", due, err)
	}
	if due, err := db.ListDueGraphDispatches(ctx, retry, 10); err != nil || len(due) != 1 {
		t.Fatalf("requeued work is not due at its retry time: %+v err=%v", due, err)
	}

	if _, _, err := db.ReserveGraphDispatch(ctx, parked.ID, retry); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishGraphDispatch(ctx, parked.ID, retry); err != nil {
		t.Fatalf("finish: %v", err)
	}
	// Factory-only stages finish without allocating an agent run.
	finished := readLaunchDispatch(t, db, parked.ID)
	if finished.Status != "finished" || finished.AllocatedRunID != 0 {
		t.Fatalf("factory-only completion: %+v", finished)
	}
	runs, err := db.ListRuns(ctx, instance.SessionID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("factory-only stage allocated a run: %+v %v", runs, err)
	}
	if due, err := db.ListDueGraphDispatches(ctx, retry, 10); err != nil || len(due) != 0 {
		t.Fatalf("a finished dispatch is still due: %+v err=%v", due, err)
	}
	after, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.NextDueAt.IsZero() {
		t.Errorf("the instance is still due with no open dispatch: %v", after.NextDueAt)
	}
}

func TestParkRefusesADisagreeingNode(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	now := time.Now().UTC()
	if _, err := db.ParkGraphInstance(t.Context(), instance.ID, "some-other-node", now, now); err == nil {
		t.Error("parking on a node the instance is not on was accepted")
	}
}

// A stage that can never start must stop asking. Retrying a malformed request
// every tick spends a forge observation a minute and learns nothing, and that
// is exactly what a live run did before this existed.
func TestGraphDispatchGivesUpAfterRepeatedFailures(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, instance := graphDispatchFixture(t)
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	parked, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= MaxGraphDispatchAttempts; attempt++ {
		reserved, ok, err := db.ReserveGraphDispatch(ctx, parked.ID, at)
		if err != nil || !ok {
			t.Fatalf("attempt %d could not be reserved: ok=%v err=%v", attempt, ok, err)
		}
		if reserved.Attempts != attempt {
			t.Fatalf("attempt %d counted as %d", attempt, reserved.Attempts)
		}
		at = at.Add(time.Hour)
		requeued, err := db.RequeueGraphDispatch(ctx, parked.ID, at, "free_text has no instruction text")
		if err != nil {
			t.Fatal(err)
		}
		if attempt < MaxGraphDispatchAttempts && !requeued {
			t.Fatalf("attempt %d gave up early", attempt)
		}
		if attempt == MaxGraphDispatchAttempts && requeued {
			t.Fatal("a dispatch retried past its cap")
		}
	}
	final, err := db.ListDueGraphDispatches(ctx, at.Add(24*time.Hour), 10)
	if err != nil || len(final) != 0 {
		t.Fatalf("an exhausted dispatch is still due: %+v err=%v", final, err)
	}
	after, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The stage failed; the work it already did did not.
	if after.Status != GraphRunning || after.CurrentNode != instance.CurrentNode || !after.NextDueAt.IsZero() {
		t.Fatalf("giving up must leave the instance parked and quiet: %+v", after)
	}
}

func TestGraphDispatchRetryDelayGrows(t *testing.T) {
	t.Parallel()
	if GraphDispatchRetryDelay(1) >= GraphDispatchRetryDelay(2) {
		t.Error("a repeated failure must wait longer than the first")
	}
	if GraphDispatchRetryDelay(0) <= 0 {
		t.Error("a retry delay must be positive")
	}
}

func TestGraphDispatchStopOutcomeIsOnlyActualExhaustion(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	ctx, at := t.Context(), graphTestTime()
	dispatch, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= MaxGraphDispatchAttempts; attempt++ {
		if _, ok, err := db.ReserveGraphDispatch(ctx, dispatch.ID, at); err != nil || !ok {
			t.Fatalf("reserve: %v %v", ok, err)
		}
		requeued, stopped, err := db.RequeueGraphDispatchWithStop(ctx, dispatch.ID, at, "startup refused")
		if err != nil {
			t.Fatal(err)
		}
		if attempt < MaxGraphDispatchAttempts {
			if !requeued || stopped != nil {
				t.Fatalf("premature stop: %v %+v", requeued, stopped)
			}
		} else if requeued || stopped == nil || stopped.Status != "cancelled" || stopped.Attempts != attempt || stopped.CancellationReason == "" {
			t.Fatalf("missing stop: %v %+v", requeued, stopped)
		}
	}
	if requeued, stopped, err := db.RequeueGraphDispatchWithStop(ctx, dispatch.ID, at, "replay"); err != nil || requeued || stopped != nil {
		t.Fatalf("no-op announced another stop: %v %+v %v", requeued, stopped, err)
	}
	other, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CancelGraphDispatch(ctx, other.ID, "issue closed", at); err != nil {
		t.Fatal(err)
	}
	if _, stopped, err := db.RequeueGraphDispatchWithStop(ctx, other.ID, at, "replay"); err != nil || stopped != nil {
		t.Fatalf("closure announced: %+v %v", stopped, err)
	}
}

func TestGraphReplyRecoveryAtomicallyConsumesInput(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx, at := t.Context(), graphTestTime()
	session := testSession(t, db, 466)
	pin := graphTestDefinition(t, db)
	if _, err := db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	admitted, err := db.AdmitWorkflowLaunch(ctx, WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: 1, DefinitionID: pin.ID, Start: "work", ContentDigest: pin.Digest, TopologyDigest: pin.Digest, Files: []byte(`{"graph.dot":"eA=="}`)})
	if err != nil {
		t.Fatal(err)
	}
	instance := admitted.Instance
	old, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CancelGraphDispatch(ctx, old.ID, "stopped", at); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.ParkGraphInstanceForReply(ctx, instance.ID, instance.CurrentNode, at, 10)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == old.ID || fresh.Attempts != 0 || fresh.InstanceID != instance.ID || fresh.Node != instance.CurrentNode {
		t.Fatalf("wrong recovery: %+v", fresh)
	}
	wm, exists, err := db.GetWatermark(ctx, session.ID)
	if err != nil || !exists || wm.LastCommentID != 10 {
		t.Fatalf("not consumed: %+v %v", wm, err)
	}
	if err = db.CancelGraphDispatch(ctx, fresh.ID, "later stop", at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ParkGraphInstanceForReply(ctx, instance.ID, instance.CurrentNode, at, 10); err != nil {
		t.Fatal(err)
	}
	latest, err := db.LatestGraphDispatch(ctx, instance.ID)
	if err != nil || latest.ID != fresh.ID || latest.Status != "cancelled" {
		t.Fatalf("replay revived a later stop: %+v %v", latest, err)
	}
	got, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil || got.DefinitionID != instance.DefinitionID || got.CurrentNode != instance.CurrentNode || got.Status != GraphRunning || !got.NextDueAt.IsZero() {
		t.Fatalf("changed retained work: %+v %v", got, err)
	}
	if _, err = db.ParkGraphInstanceForReply(ctx, instance.ID, "wrong node", at, 11); err == nil {
		t.Fatal("wrong node admitted")
	}
	wm, _, err = db.GetWatermark(ctx, session.ID)
	if err != nil || wm.LastCommentID != 10 {
		t.Fatalf("failed park consumed input: %+v %v", wm, err)
	}
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, Limit: 1, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ParkGraphInstanceForReply(ctx, instance.ID, instance.CurrentNode, at, 11); err == nil {
		t.Fatal("active attempt recovered")
	}
	findings := []byte(`{"record":"retained_findings","value":"keep this"}`)
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: at, Outputs: []GraphOutput{{Kind: "json", Payload: findings}}}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, e := db.ParkGraphInstanceForReply(ctx, instance.ID, instance.CurrentNode, at, 12)
			results <- e
		}()
	}
	for range 2 {
		select {
		case e := <-results:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("waiting for concurrent reply recovery")
		}
	}
	due, err := db.ListDueGraphDispatches(ctx, at, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("duplicate recovery: %+v %v", due, err)
	}
	retained, err := db.LatestGraphRecord(ctx, instance.ID, "retained_findings")
	if err != nil || string(retained) != string(findings) {
		t.Fatalf("lost findings: %s %v", retained, err)
	}
	if _, err = db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, Limit: 1, StartedAt: at}); !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("recovery granted node budget: %v", err)
	}
	reserved, ok, err := db.ReserveGraphDispatch(ctx, due[0].ID, at)
	if err != nil || !ok {
		t.Fatalf("reserve recovered work: %v %v", ok, err)
	}
	if err = db.ValidateGraphDispatchTarget(ctx, session.ID, reserved.TriggerID()); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.AdmitWorkflowLaunch(ctx, WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: reserved.TriggerID(), DefinitionID: pin.ID, Start: "work", ContentDigest: pin.Digest, TopologyDigest: pin.Digest, Files: []byte(`{"graph.dot":"eA=="}`)})
	if err != nil || resumed.Instance.ID != instance.ID || resumed.LaunchID != admitted.LaunchID {
		t.Fatalf("dispatch readmitted work: %+v %v", resumed, err)
	}
	if _, err = db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "work", StartedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateGraphDispatchTarget(ctx, session.ID, reserved.TriggerID()); err == nil {
		t.Fatal("old dispatch accepted newer instance")
	}
}

func interruptedDispatchFixture(t *testing.T) (*DB, GraphDispatch, Run) {
	t.Helper()
	db, launch := captureFixture(t)
	ctx := t.Context()
	admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	at := graphTestTime()
	d, err := db.ParkGraphInstance(ctx, admitted.Instance.ID, admitted.Instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.ReserveGraphDispatch(ctx, d.ID, at); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	segment, err := db.GetOrCreateSegment(ctx, d.SessionID, 1, "dispatch-recovery", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: d.SessionID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: d.TriggerID(), StartedAt: at, LogPath: func(int) string { return "recovery.log" }})
	if err != nil {
		t.Fatal(err)
	}
	return db, readLaunchDispatch(t, db, d.ID), run
}

func TestReconcileGraphDispatchTerminalRuns(t *testing.T) {
	for _, state := range []string{"succeeded", "failed", "reported success", "reported abandonment", "missing", "restart"} {
		t.Run(state, func(t *testing.T) {
			db, old, run := interruptedDispatchFixture(t)
			ctx, at := t.Context(), graphTestTime().Add(time.Hour)
			if state == "restart" {
				if _, err := db.ReconcileOrphans(ctx, "replacement-daemon"); err != nil {
					t.Fatal(err)
				}
			} else {
				outcome := RunOutcome{Status: RunSucceeded}
				if state == "failed" || state == "reported abandonment" {
					outcome.Status, outcome.Error = RunFailed, ReasonAbandoned
				}
				if _, err := db.FinishRun(ctx, run.ID, outcome); err != nil {
					t.Fatal(err)
				}
				if state == "reported success" || state == "reported abandonment" {
					if _, err := db.sql.ExecContext(ctx, `UPDATE runs SET status='reported' WHERE id=?`, run.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			history, err := db.GetRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state == "missing" {
				// Model corruption explicitly: ordinary foreign keys prohibit this state.
				if _, err = db.sql.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(ctx, `DELETE FROM runs WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
					t.Fatal(err)
				}
			}
			due := at.Add(time.Hour)
			if _, err = db.sql.ExecContext(ctx, `UPDATE graph_instances SET current_node='next',next_due_at=? WHERE id=?`, FormatTime(due), old.InstanceID); err != nil {
				t.Fatal(err)
			}
			candidates, err := db.ListInterruptedGraphDispatches(ctx)
			if err != nil || len(candidates) != 1 {
				t.Fatalf("candidates=%+v err=%v", candidates, err)
			}
			for range 2 {
				if err = db.ReconcileGraphDispatch(ctx, old.ID, old.LaunchID, at); err != nil {
					t.Fatal(err)
				}
			}
			spent := readLaunchDispatch(t, db, old.ID)
			if spent.Status != "finished" || spent.AllocatedRunID != run.ID {
				t.Fatalf("history lost: %+v", spent)
			}
			next, err := db.LatestGraphDispatch(ctx, old.InstanceID)
			if err != nil || next.ID == old.ID || next.TriggerID() == old.TriggerID() || next.Node != "next" || next.Status != "pending" || next.LaunchID != old.LaunchID || !next.DueAt.Equal(due) {
				t.Fatalf("successor=%+v err=%v", next, err)
			}
			if before, err := db.ListDueGraphDispatches(ctx, at, 10); err != nil || len(before) != 0 {
				t.Fatalf("backoff lost: %+v %v", before, err)
			}
			if ready, err := db.ListDueGraphDispatches(ctx, due, 10); err != nil || len(ready) != 1 {
				t.Fatalf("due=%+v %v", ready, err)
			}
			if state != "missing" {
				after, err := db.GetRun(ctx, run.ID)
				if err != nil || after.Status != history.Status || after.Error != history.Error || !after.FinishedAt.Equal(history.FinishedAt) {
					t.Fatalf("terminal run changed: %+v %v", after, err)
				}
			}
		})
	}
}

func TestReconcileGraphDispatchGuardsAndRollback(t *testing.T) {
	for _, guard := range []string{"live", "doctor", "attempt", "effect", "waiting", "claimed", "terminal", "superseded", "unsupported", "write failure", "read failure"} {
		t.Run(guard, func(t *testing.T) {
			db, d, run := interruptedDispatchFixture(t)
			ctx, at := t.Context(), graphTestTime().Add(time.Hour)
			if guard != "live" {
				if _, err := db.FinishRun(ctx, run.ID, RunOutcome{Status: RunFailed, Error: ReasonAbandoned}); err != nil {
					t.Fatal(err)
				}
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.sql.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch guard {
			case "doctor":
				session, err := db.GetSession(ctx, d.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				_, presentation := retainedPollProposal(t, db, session)
				if _, err = db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "dispatch-hold", SourceID: "dispatch-hold-input", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation}); err != nil {
					t.Fatal(err)
				}
			case "attempt", "effect", "waiting", "claimed":
				attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: d.InstanceID, LaunchID: d.LaunchID, Node: d.Node, Limit: 3, StartedAt: at})
				if err != nil {
					t.Fatal(err)
				}
				if guard != "attempt" {
					if err = db.FinishGraphNodeAttempt(ctx, attempt.ID, GraphSucceeded, "success", at); err != nil {
						t.Fatal(err)
					}
				}
				if guard == "effect" {
					exec(`INSERT INTO graph_effects(operation,instance_id,source_attempt_id,kind,binding,state,created_at,updated_at) VALUES('pending',?,?,'test',X'','uncertain',?,?)`, d.InstanceID, attempt.ID, FormatTime(at), FormatTime(at))
				}
				if guard == "waiting" || guard == "claimed" {
					if err = db.RetainGraphComment(ctx, GraphCommentIntent{Key: "wait", InstanceID: d.InstanceID, SourceAttemptID: attempt.ID, Node: d.Node, Kind: "question", Candidate: []byte(`{}`)}); err != nil {
						t.Fatal(err)
					}
					if guard == "claimed" {
						exec(`UPDATE graph_owner_waits SET state='claimed' WHERE instance_id=?`, d.InstanceID)
					}
				}
			case "terminal":
				if err := db.SetGraphInstanceStatus(ctx, d.InstanceID, GraphSucceeded, time.Time{}, at); err != nil {
					t.Fatal(err)
				}
			case "superseded":
				instance, err := db.GetGraphInstance(ctx, d.InstanceID)
				if err != nil {
					t.Fatal(err)
				}
				instance.ID = 0
				if _, err = db.CreateGraphInstance(ctx, instance); err != nil {
					t.Fatal(err)
				}
			case "write failure":
				exec(`CREATE TRIGGER fail_recovery BEFORE INSERT ON graph_dispatches BEGIN SELECT RAISE(ABORT,'test write failure'); END`)
			case "read failure":
				exec(`ALTER TABLE graph_effects RENAME TO unavailable_effects`)
			}
			capture := d.LaunchID
			if guard == "unsupported" {
				capture = 0
			}
			err := db.ReconcileGraphDispatch(ctx, d.ID, capture, at)
			failed := guard == "write failure" || guard == "read failure"
			if (err != nil) != failed {
				t.Fatalf("error=%v", err)
			}
			want := "running"
			if guard == "waiting" || guard == "claimed" || guard == "terminal" || guard == "superseded" {
				want = "finished"
			}
			if got := readLaunchDispatch(t, db, d.ID); got.Status != want {
				t.Fatalf("dispatch=%+v want=%s", got, want)
			}
			if next, err := db.LatestGraphDispatch(ctx, d.InstanceID); err != nil || next.ID != d.ID {
				t.Fatalf("unauthorized successor: %+v %v", next, err)
			}
			if guard == "waiting" || guard == "claimed" {
				wait, err := db.GetGraphOwnerWait(ctx, d.InstanceID)
				if err != nil || wait.State != guard {
					t.Fatalf("wait changed: %+v %v", wait, err)
				}
			}
			if failed {
				if guard == "write failure" {
					exec(`DROP TRIGGER fail_recovery`)
				} else {
					exec(`ALTER TABLE unavailable_effects RENAME TO graph_effects`)
				}
				if err = db.ReconcileGraphDispatch(ctx, d.ID, d.LaunchID, at); err != nil {
					t.Fatal(err)
				}
				if next, err := db.LatestGraphDispatch(ctx, d.InstanceID); err != nil || next.ID == d.ID {
					t.Fatalf("retry lost: %+v %v", next, err)
				}
			}
		})
	}
}

func TestStrandedGraphDispatchEligibilityAndRecheck(t *testing.T) {
	for _, guard := range []string{"idle", "pending dispatch", "reserved dispatch", "running dispatch", "cancelled", "pending attempt", "running attempt", "prelaunch run", "running run", "waiting", "claimed", "allocated", "scheduled", "running schedule", "due", "doctor", "effect", "terminal", "superseded", "node changed", "capture changed"} {
		t.Run(guard, func(t *testing.T) {
			db, launch := captureFixture(t)
			ctx, at := t.Context(), graphTestTime()
			admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := db.ListStrandedGraphDispatches(ctx)
			if err != nil || len(candidates) != 1 {
				t.Fatalf("candidates=%+v err=%v", candidates, err)
			}
			d := candidates[0]
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.sql.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch guard {
			case "pending dispatch", "reserved dispatch", "running dispatch", "cancelled":
				status := map[string]string{"pending dispatch": "pending", "reserved dispatch": "reserved", "running dispatch": "running", "cancelled": "cancelled"}[guard]
				exec(`INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id) VALUES(?,?,?,?,?,?,?)`, d.InstanceID, d.SessionID, d.Node, FormatTime(at), status, FormatTime(at), d.LaunchID)
			case "pending attempt", "running attempt", "effect", "waiting", "claimed", "allocated":
				a, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: d.InstanceID, LaunchID: d.LaunchID, Node: d.Node, Limit: 3, StartedAt: at})
				if err != nil {
					t.Fatal(err)
				}
				if guard == "pending attempt" {
					exec(`UPDATE graph_node_attempts SET status='pending' WHERE id=?`, a.ID)
				}
				if guard != "pending attempt" && guard != "running attempt" {
					if err := db.FinishGraphNodeAttempt(ctx, a.ID, GraphSucceeded, "success", at); err != nil {
						t.Fatal(err)
					}
				}
				if guard == "effect" {
					exec(`INSERT INTO graph_effects(operation,instance_id,source_attempt_id,kind,binding,state,created_at,updated_at) VALUES('pending',?,?,'test',X'','uncertain',?,?)`, d.InstanceID, a.ID, FormatTime(at), FormatTime(at))
				}
				if guard == "waiting" || guard == "claimed" || guard == "allocated" {
					if err := db.RetainGraphComment(ctx, GraphCommentIntent{Key: "wait", InstanceID: d.InstanceID, SourceAttemptID: a.ID, Node: d.Node, Kind: "question", Candidate: []byte(`{}`)}); err != nil {
						t.Fatal(err)
					}
					exec(`UPDATE graph_owner_waits SET state=? WHERE instance_id=?`, guard, d.InstanceID)
				}
			case "prelaunch run", "running run":
				segment, err := db.GetOrCreateSegment(ctx, d.SessionID, 1, "work", AgentClaude)
				if err != nil {
					t.Fatal(err)
				}
				run, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: d.SessionID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: 10, LogPath: func(int) string { return "test.log" }})
				if err != nil {
					t.Fatal(err)
				}
				if guard == "prelaunch run" {
					exec(`UPDATE runs SET status='prelaunch' WHERE id=?`, run.ID)
				}
			case "scheduled", "running schedule":
				status := "pending"
				if guard == "running schedule" {
					status = "running"
				}
				exec(`INSERT INTO schedules(session_id,source_comment_id,owner_handle,payload,status,created_at,updated_at) VALUES(?,1,'owner','test',?,?,?)`, d.SessionID, status, FormatTime(at), FormatTime(at))
			case "due":
				exec(`UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(at.Add(time.Hour)), d.InstanceID)
			case "doctor":
				session, err := db.GetSession(ctx, d.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				_, presentation := retainedPollProposal(t, db, session)
				if _, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "stranded-hold", SourceID: "stranded-input", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation}); err != nil {
					t.Fatal(err)
				}
			case "terminal":
				if err := db.SetGraphInstanceStatus(ctx, d.InstanceID, GraphSucceeded, time.Time{}, at); err != nil {
					t.Fatal(err)
				}
			case "superseded":
				if _, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: d.SessionID, DefinitionID: admitted.Instance.DefinitionID, CurrentNode: d.Node, StartedAt: at}); err != nil {
					t.Fatal(err)
				}
			case "node changed":
				exec(`UPDATE graph_instances SET current_node='other' WHERE id=?`, d.InstanceID)
			case "capture changed":
				d.LaunchID++
			}
			selected, err := db.ListStrandedGraphDispatches(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, candidate := range selected {
				if candidate.InstanceID == d.InstanceID {
					found = true
				}
			}
			wantSelected := guard == "idle" || guard == "allocated" || guard == "node changed" || guard == "capture changed"
			if found != wantSelected {
				t.Fatalf("discovery selected=%v want %v", found, wantSelected)
			}

			for range 2 {
				if err := db.ReconcileStrandedGraphDispatch(ctx, d, at); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_dispatches WHERE instance_id=?`, d.InstanceID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 0
			if guard == "idle" || guard == "allocated" || guard == "pending dispatch" || guard == "reserved dispatch" || guard == "running dispatch" || guard == "cancelled" {
				want = 1
			}
			if count != want {
				t.Fatalf("dispatch count=%d want %d", count, want)
			}
			if guard == "idle" || guard == "allocated" {
				got, err := db.LatestGraphDispatch(ctx, d.InstanceID)
				if err != nil || got.Node != d.Node || got.LaunchID != d.LaunchID || got.Status != "pending" || !got.DueAt.Equal(at) {
					t.Fatalf("dispatch=%+v err=%v", got, err)
				}
				if guard == "allocated" {
					wait, err := db.GetGraphOwnerWait(ctx, d.InstanceID)
					if err != nil || wait.State != "allocated" {
						t.Fatalf("wait changed: %+v %v", wait, err)
					}
				}
			}
		})
	}
}

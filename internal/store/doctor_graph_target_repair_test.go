package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func targetedRepairReply(t *testing.T, db *DB, session Session, body string) {
	t.Helper()
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE comments SET body=? WHERE session_id=? AND forge_comment_id=99`, body, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_owner_waits SET reply_body=? WHERE delivery_key='atomic-wait'`, body); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorGraphTargetedHandoffLateFailureRestoresEverything(t *testing.T) {
	db, instance, session, steps := graphAtomicFixture(t)
	target := OwnerInputTarget{InstanceID: instance.ID, Node: "target_worker"}
	body := fmt.Sprintf("Input-Target: instance=%d node=%s\n  Keep the exact targeted instruction.  \n", target.InstanceID, target.Node)
	targetedRepairReply(t, db, session, body)
	for index, step := range steps {
		action, err := db.PrepareDoctorGraphAction(t.Context(), session.TaskRef, step.Kind, step.Target, "Preserve the targeted reply in the complete repair")
		if err != nil {
			t.Fatal(err)
		}
		steps[index].Before, steps[index].After = action.Before, action.After
	}
	op := graphApproveSteps(t, db, session, "targeted-late-failure", steps)
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER fail_targeted_journal BEFORE UPDATE OF state ON doctor_repair_steps WHEN NEW.state='verified' AND NEW.number=4 BEGIN SELECT RAISE(ABORT,'targeted last journal failure'); END`); err != nil {
		t.Fatal(err)
	}
	before := graphFailureSnapshot(t, db)
	if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err == nil || !strings.Contains(err.Error(), "targeted last journal failure") {
		t.Fatalf("expected late failure after targeted handoff, got %v", err)
	}
	if after := graphFailureSnapshot(t, db); after != before {
		t.Fatal("late failure leaked targeted input, changed rows or numbered journals")
	}
	if _, err := db.GetOwnerInstruction(t.Context(), session.ID, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("aborted handoff left an input: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER fail_targeted_journal`); err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	input, err := db.GetOwnerInstruction(t.Context(), session.ID, 99)
	if err != nil || input.Target != target || input.Body != body || input.AttemptID != 0 {
		t.Fatalf("retry changed target, exact bytes or undelivered state: %+v %v", input, err)
	}
}

func TestDoctorGraphTargetRepairRefusesInvalidMissingInputTarget(t *testing.T) {
	for _, invalid := range []string{"malformed", "cross-session", "mismatched-launch"} {
		t.Run(invalid, func(t *testing.T) {
			db, instance, session, _ := graphAtomicFixture(t)
			body := "Input-Target: instance=invalid node=worker\nRetain this reply."
			if invalid == "cross-session" {
				otherSession := testSession(t, db, 316)
				other, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: otherSession.ID, DefinitionID: graphTestDefinition(t, db).ID, CurrentNode: "worker", StartedAt: graphTestTime()})
				if err != nil {
					t.Fatal(err)
				}
				body = fmt.Sprintf("Input-Target: instance=%d node=worker\nDo not deliver across issue ownership.", other.ID)
			}
			if invalid == "mismatched-launch" {
				other, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: instance.DefinitionID, CurrentNode: "worker", StartedAt: graphTestTime()})
				if err != nil {
					t.Fatal(err)
				}
				result, err := db.sql.ExecContext(t.Context(), `INSERT INTO research_launch_definitions(session_id,trigger_id,instance_id,content_digest,topology_digest,files) VALUES(?,?,?,'content','topology',X'7B7D')`, session.ID, 99, other.ID)
				if err != nil {
					t.Fatal(err)
				}
				launchID, err := result.LastInsertId()
				if err != nil {
					t.Fatal(err)
				}
				body = fmt.Sprintf("Input-Target: instance=%d node=worker launch=%d\nRetain exact receiver ownership.", instance.ID, launchID)
			}
			targetedRepairReply(t, db, session, body)
			before := graphFailureSnapshot(t, db)
			wantRefusal := map[string]string{"malformed": "invalid owner input target identity", "cross-session": "target belongs to another task", "mismatched-launch": "launch differs from target"}[invalid]
			_, err := db.PrepareDoctorGraphAction(t.Context(), session.TaskRef, "graph_wait_cancel", "owner_wait:atomic-wait", "Preserve valid authenticated reply guidance")
			if err != nil {
				if !strings.Contains(err.Error(), wantRefusal) {
					t.Fatalf("proposal failed without proving target refusal %q: %v", wantRefusal, err)
				}
				if after := graphFailureSnapshot(t, db); after != before {
					t.Fatal("rejected proposal changed retained state")
				}
				return
			}
			if invalid == "malformed" {
				t.Fatal("malformed explicit target admitted into exact proposal")
			}
			// Include dependent blockers so refusal proves target ownership, not
			// merely the presence of the fixture's live attempt or dispatch.
			var steps []DoctorRepairStep
			for index, item := range []struct{ kind, target string }{{"graph_attempt_cancel", ""}, {"graph_dispatch_cancel", ""}, {"graph_wait_cancel", "owner_wait:atomic-wait"}} {
				target := item.target
				if target == "" {
					var id int64
					query := `SELECT id FROM graph_node_attempts WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?)`
					prefix := "graph_attempt"
					if item.kind == "graph_dispatch_cancel" {
						query, prefix = `SELECT id FROM graph_dispatches WHERE session_id=?`, "graph_dispatch"
					}
					if err := db.sql.QueryRowContext(t.Context(), query, session.ID).Scan(&id); err != nil {
						t.Fatal(err)
					}
					target = fmt.Sprintf("%s:%d", prefix, id)
				}
				action, err := db.PrepareDoctorGraphAction(t.Context(), session.TaskRef, item.kind, target, "Refuse cross-session targeted guidance")
				if err != nil {
					t.Fatal(err)
				}
				steps = append(steps, DoctorRepairStep{Number: index + 1, Kind: item.kind, Target: target, Before: action.Before, After: action.After})
			}
			op := graphApproveSteps(t, db, session, "invalid-target", steps)
			before = graphFailureSnapshot(t, db)
			if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err == nil || !strings.Contains(err.Error(), wantRefusal) {
				t.Fatalf("invalid target was not refused by ownership: %v", err)
			}
			if after := graphFailureSnapshot(t, db); after != before {
				t.Fatal("cross-session handoff refusal partially changed repair state")
			}
		})
	}
}

func TestDoctorGraphTargetRepairCannotReachWrongReceiver(t *testing.T) {
	db, instance, session, _ := graphAtomicFixture(t)
	body := fmt.Sprintf("Input-Target: instance=%d node=target_worker\nDeliver only to the named step.", instance.ID)
	targetedRepairReply(t, db, session, body)
	// Leave only the claimed Wait as a stale blocker, so the approved repair
	// cancels it while the instance remains available for supported continuation.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_node_attempts SET status='cancelled',finished_at=? WHERE instance_id=?`, FormatTime(graphTestTime()), instance.ID); err != nil {
		t.Fatal(err)
	}
	var dispatchID int64
	if err := db.sql.QueryRowContext(t.Context(), `SELECT id FROM graph_dispatches WHERE instance_id=?`, instance.ID).Scan(&dispatchID); err != nil {
		t.Fatal(err)
	}
	if err := db.CancelGraphDispatch(t.Context(), dispatchID, "fixture stopped", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	action, err := db.PrepareDoctorGraphAction(t.Context(), session.TaskRef, "graph_wait_cancel", "owner_wait:atomic-wait", "Retain the missing targeted input without delivering it")
	if err != nil {
		t.Fatal(err)
	}
	op := graphApproveSteps(t, db, session, "targeted-receiver", []DoctorRepairStep{{Number: 1, Kind: action.Kind, Target: "owner_wait:atomic-wait", Before: action.Before, After: action.After}})
	if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleDoctorTypedRepair(t.Context(), op.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 150)
	wrong, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime(), Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	input, err := db.GetOwnerInstruction(t.Context(), session.ID, 99)
	if err != nil || input.AttemptID != 0 || input.Target.Node != "target_worker" || input.Body != body {
		t.Fatalf("wrong receiver consumed or changed repaired input: %+v %v", input, err)
	}
	assigned, err := db.OwnerInstructionsForAttempt(t.Context(), wrong.ID)
	if err != nil || len(assigned) != 0 {
		t.Fatalf("wrong receiver saw targeted instruction: %+v %v", assigned, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_node_attempts SET status='cancelled',finished_at=? WHERE id=?`, FormatTime(graphTestTime()), wrong.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_instances SET current_node='target_worker' WHERE id=?`, instance.ID); err != nil {
		t.Fatal(err)
	}
	instance.CurrentNode = "target_worker"
	right, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime(), Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	assigned, err = db.OwnerInstructionsForAttempt(t.Context(), right.ID)
	if err != nil || len(assigned) != 1 || assigned[0].Body != body || assigned[0].AttemptID != right.ID {
		t.Fatalf("matching receiver lost repaired targeted input: %+v %v", assigned, err)
	}
}

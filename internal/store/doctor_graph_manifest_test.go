package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func graphApproveSteps(t *testing.T, db *DB, session Session, id string, steps []DoctorRepairStep) DoctorRepair {
	t.Helper()
	ctx := t.Context()
	attemptID := "proposer-" + id
	c, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt(attemptID))
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(DoctorEvidence{TaskRef: session.TaskRef})
	if err = db.PrepareDoctorAttempt(ctx, DoctorAttempt{ID: attemptID, Provider: "claude", ConfiguredModel: "test", Evidence: string(evidence)}); err != nil {
		t.Fatal(err)
	}
	proposal := make([]struct {
		Number int                `json:"number"`
		Target string             `json:"target"`
		Action DoctorRepairAction `json:"action"`
	}, len(steps))
	for i, s := range steps {
		proposal[i].Number = s.Number
		proposal[i].Target = s.Target
		proposal[i].Action = DoctorRepairAction{Kind: s.Kind, Before: s.Before, After: s.After}
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.BindDoctorResultAttempt(ctx, attemptID)
	if err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err = db.sql.QueryRowContext(ctx, `SELECT revision FROM application_results WHERE id=?`, b.ResultID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	record := resultRecord(b, "proposal-"+id, revision)
	record.Snapshot = append(append([]byte(`{"proposal":`), payload...), '}')
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err = db.AdoptDoctorResult(ctx, b, "proposal-"+id, payload); err != nil {
		t.Fatal(err)
	}
	var pid int64
	if err = db.sql.QueryRowContext(ctx, `SELECT current_proposal_id FROM doctor_conversations WHERE id=?`, c.ID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	presentation := "present-" + id
	if err = db.PresentDoctorProposal(ctx, session.TaskRef, presentation, pid); err != nil {
		t.Fatal(err)
	}
	op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: id, SourceID: "owner-" + id, Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, id, "daemon"); err != nil {
		t.Fatal(err)
	}
	return op
}

func graphAtomicFixture(t *testing.T) (*DB, GraphInstance, Session, []DoctorRepairStep) {
	t.Helper()
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	session, err := db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	attempt := graphTestAttempt(t, db, instance, 3)
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at) VALUES(?,?,? ,?,'pending',?)`, instance.ID, session.ID, instance.CurrentNode, FormatTime(graphTestTime()), FormatTime(graphTestTime())); err != nil {
		t.Fatal(err)
	}
	var dispatchID int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT id FROM graph_dispatches WHERE instance_id=?`, instance.ID).Scan(&dispatchID); err != nil {
		t.Fatal(err)
	}
	recordOwnerInput(t, db, session.ID, 99, "Retain these exact words.\nContinue carefully.")
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate,stop_origin,stop_cause,stop_reason,created_at) VALUES('atomic-wait',?,?,?,'question',X'7B7D',?,'stopped','needs guidance',?)`, instance.ID, attempt.ID, instance.CurrentNode, instance.CurrentNode, FormatTime(graphTestTime())); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state,reply_id,reply_body,reply_author_id,claimed_at,origin_node,allow_runless) VALUES('atomic-wait',?,?,'claimed',99,?,42,?,?,1)`, instance.ID, attempt.ID, "Retain these exact words.\nContinue carefully.", FormatTime(graphTestTime()), instance.CurrentNode); err != nil {
		t.Fatal(err)
	}
	kinds := []string{"graph_fail", "graph_wait_cancel", "graph_dispatch_cancel", "graph_attempt_cancel"}
	targets := []string{fmt.Sprintf("graph:%d", instance.ID), "owner_wait:atomic-wait", fmt.Sprintf("graph_dispatch:%d", dispatchID), fmt.Sprintf("graph_attempt:%d", attempt.ID)}
	var steps []DoctorRepairStep
	for i, kind := range kinds {
		a, err := db.PrepareDoctorGraphAction(t.Context(), session.TaskRef, kind, targets[i], "Remove confirmed stale blockers")
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, DoctorRepairStep{Number: i + 1, Kind: kind, Target: targets[i], Before: a.Before, After: a.After})
	}
	return db, instance, session, steps
}

func TestDoctorGraphWholeRepairAndGuardedInverse(t *testing.T) {
	db, instance, session, steps := graphAtomicFixture(t)
	ctx := t.Context()
	op := graphApproveSteps(t, db, session, "forward", steps)
	if err := db.BackupDoctorRows(ctx, op.ID, 1); err == nil {
		t.Fatal("split graph backup admitted")
	}
	if err := db.ApplyDoctorRows(ctx, op.ID, 1); err == nil {
		t.Fatal("split graph write admitted")
	}
	if err := db.ApplyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal("lost receipt replay", err)
	}
	if err := db.VerifyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	inputs, err := db.ListOwnerInstructions(ctx, session.ID, 0)
	if err != nil || len(inputs) != 1 || inputs[0].Body != "Retain these exact words.\nContinue carefully." {
		t.Fatal("guidance not retained once", inputs, err)
	}
	journal, err := db.DoctorRepairSteps(ctx, op.ID)
	if err != nil || len(journal) != 4 {
		t.Fatal(journal, err)
	}
	for i, s := range journal {
		if s.Number != i+1 || s.State != "verified" || s.BackupReference == "" || s.BackupDigest == "" {
			t.Fatal("incomplete numbered journal", s)
		}
	}
	if err := db.SettleDoctorTypedRepair(ctx, op.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	inverse, err := db.PrepareDoctorGraphRollback(ctx, session.TaskRef, op.ID, "Restore the exact prior stopped records")
	if err != nil {
		t.Fatal(err)
	}
	rollback := graphApproveSteps(t, db, session, "inverse", inverse)
	if err := db.ApplyDoctorGraphRepair(ctx, rollback.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyDoctorGraphRepair(ctx, rollback.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleDoctorTypedRepair(ctx, rollback.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	restored, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil || restored.Status != GraphRunning {
		t.Fatal(restored, err)
	}
	inputs, err = db.ListOwnerInstructions(ctx, session.ID, 0)
	if err != nil || len(inputs) != 0 {
		t.Fatal("inverse leaked new pending handoff", inputs, err)
	}
	for _, s := range steps {
		image, _, err := graphStepImages(s)
		if err != nil {
			t.Fatal(err)
		}
		var current doctorGraphImage
		err = db.withTx(ctx, func(tx *sql.Tx) error {
			var e error
			current, e = graphTargetImage(ctx, tx, session.TaskRef, s.Kind, s.Target, image.Reason)
			return e
		})
		if err != nil || !reflect.DeepEqual(image.Row, current.Row) {
			t.Fatal("inverse changed retained metadata", s.Kind, err)
		}
	}
	if _, err := db.PrepareDoctorGraphRollback(ctx, session.TaskRef, op.ID, "Do it again"); err == nil {
		t.Fatal("old repair offered another inverse")
	}
}

func TestDoctorGraphActiveRelatedWorkRefused(t *testing.T) {
	db, _, session, steps := graphAtomicFixture(t)
	op := graphApproveSteps(t, db, session, "refuse", steps[:1])
	if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err == nil {
		t.Fatal("instance failure stranded live related work")
	}
	journal, err := db.DoctorRepairSteps(t.Context(), op.ID)
	if err != nil || journal[0].State != "pending" {
		t.Fatal("refusal wrote journal", journal, err)
	}
	if err := db.SettleDoctorTypedRepair(t.Context(), op.ID, false, "active related work"); err != nil {
		t.Fatal(err)
	}
	held, err := db.DoctorRepairHeld(t.Context(), session.TaskRef)
	if err != nil || held {
		t.Fatal("proven abort did not release hold", held, err)
	}
}

func TestDoctorGraphWaitPreservesTargetAndAssignment(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		t.Run(fmt.Sprint("assigned-", assigned), func(t *testing.T) {
			db, instance, session, steps := graphAtomicFixture(t)
			ctx := t.Context()
			target := OwnerInputTarget{InstanceID: instance.ID, Node: instance.CurrentNode}
			original, err := db.RetainOwnerInstruction(ctx, session.ID, 99, 42, "Retain these exact words.\nContinue carefully.", target)
			if err != nil {
				t.Fatal(err)
			}
			if assigned {
				var attemptID int64
				if err = db.sql.QueryRowContext(ctx, `SELECT id FROM graph_node_attempts WHERE instance_id=?`, instance.ID).Scan(&attemptID); err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(ctx, `UPDATE owner_instruction_inputs SET assigned_attempt_id=?,assigned_at=? WHERE session_id=?`, attemptID, FormatTime(graphTestTime()), session.ID); err != nil {
					t.Fatal(err)
				}
				original, err = db.GetOwnerInstruction(ctx, session.ID, 99)
				if err != nil {
					t.Fatal(err)
				}
			}
			for i, s := range steps {
				action, err := db.PrepareDoctorGraphAction(ctx, session.TaskRef, s.Kind, s.Target, "Preserve admitted target and assignment")
				if err != nil {
					t.Fatal(err)
				}
				steps[i].Before, steps[i].After = action.Before, action.After
			}
			op := graphApproveSteps(t, db, session, "preserve-binding", steps)
			if err = db.ApplyDoctorGraphRepair(ctx, op.ID); err != nil {
				t.Fatal(err)
			}
			current, err := db.GetOwnerInstruction(ctx, session.ID, 99)
			if err != nil || !reflect.DeepEqual(original, current) {
				t.Fatal("repair changed prior guidance binding", original, current, err)
			}
		})
	}
}

func TestDoctorGraphPrelaunchRunsRemainActive(t *testing.T) {
	for _, kind := range []string{"graph_attempt_cancel", "graph_dispatch_cancel", "graph_wait_cancel"} {
		t.Run(kind, func(t *testing.T) {
			db, instance, session, steps := graphAtomicFixture(t)
			ctx := t.Context()
			run := testRun(t, db, session.ID, 500)
			if _, err := db.sql.ExecContext(ctx, `UPDATE runs SET status='prelaunch' WHERE id=?`, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.sql.ExecContext(ctx, `UPDATE graph_node_attempts SET status='cancelled',finished_at=? WHERE instance_id=?`, FormatTime(graphTestTime()), instance.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.sql.ExecContext(ctx, `UPDATE graph_dispatches SET allocated_run_id=?,status='cancelled',finished_at=? WHERE instance_id=?`, run.ID, FormatTime(graphTestTime()), instance.ID); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "graph_attempt_cancel":
				if _, err := db.sql.ExecContext(ctx, `UPDATE graph_node_attempts SET status='running',finished_at=NULL,run_id=? WHERE instance_id=?`, run.ID, instance.ID); err != nil {
					t.Fatal(err)
				}
			case "graph_dispatch_cancel":
				if _, err := db.sql.ExecContext(ctx, `UPDATE graph_dispatches SET status='pending',finished_at=NULL WHERE instance_id=?`, instance.ID); err != nil {
					t.Fatal(err)
				}
			}
			var selected DoctorRepairStep
			for _, s := range steps {
				if s.Kind == kind {
					a, err := db.PrepareDoctorGraphAction(ctx, session.TaskRef, kind, s.Target, "Do not cancel active prelaunch work")
					if err != nil {
						t.Fatal(err)
					}
					selected = DoctorRepairStep{Number: 1, Kind: kind, Target: s.Target, Before: a.Before, After: a.After}
				}
			}
			op := graphApproveSteps(t, db, session, "active-prelaunch", []DoctorRepairStep{selected})
			if err := db.ApplyDoctorGraphRepair(ctx, op.ID); err == nil {
				t.Fatal("prelaunch allocated run was treated as stopped")
			}
		})
	}
}

func TestDoctorGraphManifestPreservesLargeIntegerIdentities(t *testing.T) {
	db, instance, session, steps := graphAtomicFixture(t)
	ctx := t.Context()
	const replyID int64 = 9007199254740993
	const actorID int64 = 9007199254740995
	if _, err := db.sql.ExecContext(ctx, `UPDATE comments SET forge_comment_id=?,author_user_id=? WHERE session_id=? AND forge_comment_id=99`, replyID, actorID, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE graph_owner_waits SET reply_id=?,reply_author_id=? WHERE instance_id=?`, replyID, actorID, instance.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO research_launch_definitions(session_id,trigger_id,instance_id,content_digest,topology_digest,files) VALUES(?,?,?,'content','topology',X'7B7D')`, session.ID, replyID, instance.ID); err != nil {
		t.Fatal(err)
	}
	for i, step := range steps {
		action, err := db.PrepareDoctorGraphAction(ctx, session.TaskRef, step.Kind, step.Target, "Keep integer identities exact")
		if err != nil {
			t.Fatal(err)
		}
		steps[i].Before, steps[i].After = action.Before, action.After
		image, err := graphImage(action.Before)
		if err != nil {
			t.Fatal(err)
		}
		for _, relation := range image.Relations {
			if relation.Table == "research_launch_definitions" {
				if len(relation.Rows) != 1 || relation.Rows[0]["trigger_id"] != json.Number("9007199254740993") {
					t.Fatal("related launch identity lost precision", relation)
				}
			}
		}
		if step.Kind == "graph_wait_cancel" {
			if image.Row["reply_id"] != json.Number("9007199254740993") || image.Row["reply_author_id"] != json.Number("9007199254740995") {
				t.Fatal("reply identity lost precision", image.Row)
			}
		}
	}
	op := graphApproveSteps(t, db, session, "large-ids", steps)
	if err := db.ApplyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	input, err := db.GetOwnerInstruction(ctx, session.ID, replyID)
	if err != nil || input.CommentID != replyID || input.ActorID != actorID {
		t.Fatal("guidance changed exact authenticated identities", input, err)
	}
	if err := db.SettleDoctorTypedRepair(ctx, op.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	inverse, err := db.PrepareDoctorGraphRollback(ctx, session.TaskRef, op.ID, "Restore exact large-identity records")
	if err != nil {
		t.Fatal(err)
	}
	rollback := graphApproveSteps(t, db, session, "large-ids-inverse", inverse)
	if err = db.ApplyDoctorGraphRepair(ctx, rollback.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyDoctorGraphRepair(ctx, rollback.ID); err != nil {
		t.Fatal(err)
	}
}

package store

import (
	"encoding/json"
	"fmt"
	"testing"
)

func retainedPollProposal(t *testing.T, db *DB, session Session) (int64, string) {
	t.Helper()
	return retainedRowProposal(t, db, session, []byte(fmt.Sprintf(`[{"number":1,"target":"session:%d","action":{"kind":"poll_flag","before":"0","after":"1"}}]`, session.ID)))
}

func retainedRowProposal(t *testing.T, db *DB, session Session, proposal []byte) (int64, string) {
	t.Helper()
	ctx := t.Context()
	c, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt("repair-proposer"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(DoctorEvidence{TaskRef: session.TaskRef})
	if err = db.PrepareDoctorAttempt(ctx, DoctorAttempt{ID: "repair-proposer", Provider: "claude", ConfiguredModel: "test", Evidence: string(evidence)}); err != nil {
		t.Fatal(err)
	}
	b, err := db.BindDoctorResultAttempt(ctx, "repair-proposer")
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(b, "proposal", 0)
	record.Snapshot = append(append([]byte(`{"proposal":`), proposal...), '}')
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err = db.AdoptDoctorResult(ctx, b, "proposal", proposal); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err = db.sql.QueryRowContext(ctx, `SELECT current_proposal_id FROM doctor_conversations WHERE id=?`, c.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = db.PresentDoctorProposal(ctx, session.TaskRef, "presentation", id); err != nil {
		t.Fatal(err)
	}
	return id, "presentation"
}
func TestDoctorRepairApprovalBackupsHoldAndRestartIdentity(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	sibling := testSession(t, db, 407)
	_, presentation := retainedPollProposal(t, db, session)
	input := DoctorApproval{ID: "repair-one", SourceID: "approval-one", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation}
	wrong := input
	wrong.PresentationID = "not-shown"
	if _, err := db.ReserveDoctorRepair(ctx, wrong); err == nil {
		t.Fatal("unpresented proposal accepted")
	}
	operation, err := db.ReserveDoctorRepair(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := db.ReserveDoctorRepair(ctx, input)
	if err != nil || replay.ID != operation.ID {
		t.Fatalf("approval replay changed operation: %+v %v", replay, err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE sessions SET needs_poll=1 WHERE id=?`, session.ID); err == nil {
		t.Fatal("normal writer bypassed hold")
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE sessions SET needs_poll=1 WHERE id=?`, sibling.ID); err != nil {
		t.Fatalf("sibling blocked: %v", err)
	}
	if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err == nil {
		t.Fatal("write before drain/backup accepted")
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, operation.ID, "old-daemon"); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err == nil {
		t.Fatal("write before backup accepted")
	}
	if err = db.BackupDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal("settled row replay should retain receipt", err)
	}
	var backup []byte
	if err = db.sql.QueryRowContext(ctx, `SELECT backup FROM doctor_repair_steps WHERE repair_id=?`, operation.ID).Scan(&backup); err != nil || len(backup) == 0 {
		t.Fatal("missing backup", err)
	}
	if err = db.CommitDoctorRestart(ctx, operation.ID, "restart-token", "Verified repair; copies retained"); err != nil {
		t.Fatal(err)
	}
	if err = db.StopDoctorRepair(ctx, session.TaskRef, "stop after handoff"); err != nil {
		t.Fatal(err)
	}
	intent, err := db.ClaimDoctorRestart(ctx, operation.ID)
	if err != nil || intent.Token != "restart-token" {
		t.Fatalf("restart: %+v %v", intent, err)
	}
	if _, err = db.ClaimDoctorRestart(ctx, operation.ID); err == nil {
		t.Fatal("restart was dispatched twice")
	}
	if err = db.AcknowledgeDoctorRestart(ctx, operation.ID, intent.Token, "old-daemon"); err == nil {
		t.Fatal("old daemon acknowledged restart")
	}
	if held, err := db.DoctorRepairHeld(ctx, session.TaskRef); err != nil || !held {
		t.Fatal("hold released before new instance", err)
	}
	if err = db.AcknowledgeDoctorRestart(ctx, operation.ID, intent.Token, "new-daemon"); err != nil {
		t.Fatal(err)
	}
	if held, err := db.DoctorRepairHeld(ctx, session.TaskRef); err != nil || held {
		t.Fatal("hold not released after recovery", err)
	}
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM doctor_repair_steps WHERE repair_id=?`, operation.ID); err == nil {
		t.Fatal("backup history deleted")
	}
}
func TestDoctorRepairStopRetainsPartialHoldAndRejectsFurtherWrites(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(fmt.Sprint(write), func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			session := testSession(t, db, 406)
			_, presentation := retainedPollProposal(t, db, session)
			operation, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "repair", SourceID: "approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.AcknowledgeDoctorExclusion(ctx, operation.ID, "instance"); err != nil {
				t.Fatal(err)
			}
			if err = db.BackupDoctorPoll(ctx, operation.ID, 1); err != nil {
				t.Fatal(err)
			}
			if write {
				if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.StopDoctorRepair(ctx, session.TaskRef, "owner stop"); err != nil {
				t.Fatal(err)
			}
			if held, err := db.DoctorRepairHeld(ctx, session.TaskRef); err != nil || held != write {
				t.Fatalf("wrong partial hold %v %v", held, err)
			}
			if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err == nil {
				t.Fatal("late writer accepted")
			}
			if err = db.CommitDoctorRestart(ctx, operation.ID, "restart", "done"); err == nil {
				t.Fatal("stopped repair triggered restart")
			}
		})
	}
}

package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestDoctorStopIntentBackupAndJoinedSettlement(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 980)
	run := testRun(t, db, session.ID, 1)
	op := typedRowApproval(t, db, session, "stop_run", fmt.Sprintf("run:%d", run.ID), "running", "failed")
	if err := db.CheckDoctorTargetStopped(t.Context(), session.TaskRef); err == nil {
		t.Fatal("live invocation drained")
	}
	if _, err := db.BeginDoctorStopRun(t.Context(), op.ID, 2); err == nil {
		t.Fatal("unknown step accepted")
	}
	got, err := db.BeginDoctorStopRun(t.Context(), op.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID {
		t.Fatal(got)
	}
	retained, err := db.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := retained.OwnerStop(); !ok {
		t.Fatal("owner stop missing")
	}
	if retained.Status != RunRunning {
		t.Fatal("intent terminated run before join")
	}
	if _, err = db.BeginDoctorStopRun(t.Context(), op.ID, 1); err == nil {
		t.Fatal("uncertain intent replayed")
	}
	if err = db.CompleteDoctorStopRun(t.Context(), op.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.CheckDoctorTargetStopped(t.Context(), session.TaskRef); err != nil {
		t.Fatal(err)
	}
	steps, err := db.DoctorRepairSteps(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].State != "verified" || steps[0].BackupReference == "" || steps[0].ObservedAfter != "failed" {
		t.Fatal(steps)
	}
}

func TestDoctorStopJoinedGraphAndSessionSettlement(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	session, err := db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 1)
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, RunID: run.ID, StartedAt: graphTestTime(), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	op := typedRowApproval(t, db, session, "stop_run", fmt.Sprintf("run:%d", run.ID), "running", "failed")
	if _, err = db.BeginDoctorStopRun(t.Context(), op.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteDoctorStopRun(t.Context(), op.ID, 1); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetGraphNodeAttempt(t.Context(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GraphFailed {
		t.Fatal(got)
	}
	graph, err := db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Status != "cancelled" {
		t.Fatal(graph)
	}
	session, err = db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.State != SessionFailed || session.LockOwner != "" {
		t.Fatal(session)
	}
	steps, err := db.DoctorRepairSteps(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	var backup []byte
	if err = db.sql.QueryRowContext(t.Context(), "SELECT backup FROM doctor_repair_steps WHERE repair_id=? AND number=1", op.ID).Scan(&backup); err != nil {
		t.Fatal(err)
	}
	if steps[0].BackupReference == "" || !strings.Contains(string(backup), "attempts") {
		t.Fatal("graph state not backed up")
	}
}

func TestDoctorStopRefusesWrongKindAndBeforeDrift(t *testing.T) {
	for _, shape := range []string{"kind", "drift"} {
		t.Run(shape, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 982)
			run := testRun(t, db, session.ID, 1)
			kind, target, before, after := "stop_run", fmt.Sprintf("run:%d", run.ID), "running", "failed"
			if shape == "kind" {
				kind = "cancel_schedule"
				schedule := typedSchedule(t, db, session, false)
				target = fmt.Sprintf("schedule:%d", schedule.ID)
				before = "pending"
			}
			op := typedRowApproval(t, db, session, kind, target, before, after)
			if shape == "drift" {
				if _, err := db.sql.ExecContext(t.Context(), "INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)", op.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunFailed}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.sql.ExecContext(t.Context(), "DELETE FROM doctor_repair_write_permits WHERE repair_id=?", op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.BeginDoctorStopRun(t.Context(), op.ID, 1); err == nil {
				t.Fatal("invalid stop accepted")
			}
			steps, err := db.DoctorRepairSteps(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if steps[0].State != "pending" || steps[0].BackupReference != "" {
				t.Fatal("refusal mutated step", steps)
			}
			got, err := db.GetRun(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := got.OwnerStop(); ok {
				t.Fatal("refusal recorded owner stop")
			}
		})
	}
}

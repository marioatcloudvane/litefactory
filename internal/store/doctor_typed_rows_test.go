package store

import (
	"encoding/json"
	"fmt"
	"testing"
)

func typedRowProposal(t *testing.T, db *DB, session Session, kind, target, before, after string) string {
	t.Helper()
	payload, err := json.Marshal([]struct {
		Number int                `json:"number"`
		Target string             `json:"target"`
		Action DoctorRepairAction `json:"action"`
	}{{1, target, DoctorRepairAction{Kind: kind, Before: before, After: after}}})
	if err != nil {
		t.Fatal(err)
	}
	_, presentation := retainedRowProposal(t, db, session, payload)
	return presentation
}

func typedRowApproval(t *testing.T, db *DB, session Session, kind, target, before, after string) DoctorRepair {
	t.Helper()
	presentation := typedRowProposal(t, db, session, kind, target, before, after)
	op, err := db.ReserveDoctorRepair(t.Context(), DoctorApproval{ID: "typed-repair", SourceID: "owner-apply", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorExclusion(t.Context(), op.ID, "test-daemon"); err != nil {
		t.Fatal(err)
	}
	return op
}

func typedSchedule(t *testing.T, db *DB, session Session, running bool) Schedule {
	t.Helper()
	row, _, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: session.ID, SourceCommentID: 7, OwnerHandle: "owner", Payload: "work", DueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if running {
		var ok bool
		row, ok, err = db.ReserveSchedule(t.Context(), row.ID, graphTestTime())
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	return row
}

func TestDoctorTypedScheduleAtomicBackupAndRecheck(t *testing.T) {
	for _, tc := range []struct {
		kind, before, after string
		running             bool
	}{{"requeue_schedule", "running", "pending", true}, {"cancel_schedule", "pending", "failed", false}, {"cancel_schedule", "running", "failed", true}} {
		t.Run(tc.kind+tc.before, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 640)
			schedule := typedSchedule(t, db, session, tc.running)
			op := typedRowApproval(t, db, session, tc.kind, fmt.Sprintf("schedule:%d", schedule.ID), tc.before, tc.after)
			step, err := db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			if step.State != "verified" || step.BackupReference == "" || step.BackupDigest == "" || step.ObservedAfter != tc.after {
				t.Fatalf("incomplete journal: %+v", step)
			}
			var backup []byte
			if err = db.sql.QueryRowContext(t.Context(), `SELECT backup FROM doctor_repair_steps WHERE repair_id=? AND number=1`, op.ID).Scan(&backup); err != nil || !json.Valid(backup) {
				t.Fatalf("backup %q: %v", backup, err)
			}
			var saved struct {
				Status string `json:"status"`
			}
			if err = json.Unmarshal(backup, &saved); err != nil || saved.Status != tc.before {
				t.Fatalf("backup did not retain before-state: %q, %v", backup, err)
			}
			got, err := db.GetSchedule(t.Context(), schedule.ID)
			if err != nil || string(got.Status) != tc.after {
				t.Fatal(got, err)
			}
			replay, err := db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, tc.kind)
			if err != nil || replay.BackupDigest != step.BackupDigest {
				t.Fatal("replay replaced backup", replay, err)
			}
		})
	}
}

func TestDoctorTypedRowsRefusalLeavesJournalAndTargetUntouched(t *testing.T) {
	for _, mode := range []string{"wrong-kind", "unknown-step", "no-drain", "drift"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 641)
			schedule := typedSchedule(t, db, session, true)
			presentation := typedRowProposal(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
			op, err := db.ReserveDoctorRepair(t.Context(), DoctorApproval{ID: "typed-repair", SourceID: "owner-apply", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
			if err != nil {
				t.Fatal(err)
			}
			if mode != "no-drain" {
				if err = db.AcknowledgeDoctorExclusion(t.Context(), op.ID, "test-daemon"); err != nil {
					t.Fatal(err)
				}
			}
			number, kind := 1, "requeue_schedule"
			if mode == "wrong-kind" {
				kind = "cancel_schedule"
			}
			if mode == "unknown-step" {
				number = 2
			}
			if mode == "drift" {
				if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, op.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(t.Context(), `UPDATE schedules SET status='failed' WHERE id=?`, schedule.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(t.Context(), `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, op.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := db.GetSchedule(t.Context(), schedule.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.ApplyDoctorTypedRows(t.Context(), op.ID, number, kind); err == nil {
				t.Fatal("refusal accepted")
			}
			after, err := db.GetSchedule(t.Context(), schedule.ID)
			if err != nil || before.Status != after.Status {
				t.Fatal("refusal mutated schedule", before, after, err)
			}
			steps, err := db.DoctorRepairSteps(t.Context(), op.ID)
			if err != nil || len(steps) != 1 || steps[0].State != "pending" || steps[0].BackupReference != "" {
				t.Fatal("refusal mutated journal", steps, err)
			}
		})
	}
}

func TestDoctorTypedReleaseRetainsHistory(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	session, err := db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	op := typedRowApproval(t, db, session, "release_instance", fmt.Sprintf("graph:%d", instance.ID), "running", "failed")
	step, err := db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, "release_instance")
	if err != nil || step.State != "verified" || step.BackupReference == "" {
		t.Fatal(step, err)
	}
	var status string
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT status FROM graph_instances WHERE id=?`, instance.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatal(status, err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM graph_instances WHERE id=?`, instance.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("history removed", count, err)
	}
}

func TestDoctorTypedScheduleCannotCrossTask(t *testing.T) {
	db := newTestDB(t)
	selected := testSession(t, db, 642)
	other := testSession(t, db, 643)
	schedule := typedSchedule(t, db, other, true)
	presentation := typedRowProposal(t, db, selected, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
	if _, err := db.ReserveDoctorRepair(t.Context(), DoctorApproval{ID: "foreign", SourceID: "apply", Principal: "local-owner", TaskRef: selected.TaskRef, PresentationID: presentation}); err == nil {
		t.Fatal("foreign schedule reserved")
	}
	row, err := db.GetSchedule(t.Context(), schedule.ID)
	if err != nil || row.Status != ScheduleRunning {
		t.Fatal(row, err)
	}
	held, err := db.DoctorRepairHeld(t.Context(), selected.TaskRef)
	if err != nil || held {
		t.Fatal("refusal left hold", held, err)
	}
}

func TestDoctorTypedStoryTransitionsRetainBackup(t *testing.T) {
	for _, kind := range []string{"resume_story", "park_story"} {
		t.Run(kind, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 644)
			ctx := t.Context()
			if err := db.EnsureImplementStories(ctx, session.TaskRef, 1, []ImplementStorySeed{{Key: "one", Branch: "lf/one"}}, implementT0); err != nil {
				t.Fatal(err)
			}
			row, err := db.TransitionImplementStory(ctx, ImplementTransition{Parent: session.TaskRef, Generation: 1, Key: "one", From: ImplementWaiting, To: ImplementRunning, ExpectedRevision: 1, At: implementT0})
			if err != nil {
				t.Fatal(err)
			}
			before, after := "running", "parked"
			if kind == "resume_story" {
				row, err = db.TransitionImplementStory(ctx, ImplementTransition{Parent: session.TaskRef, Generation: 1, Key: "one", From: ImplementRunning, To: ImplementParked, ExpectedRevision: row.Revision, Reason: "owner decision", At: implementT0})
				if err != nil {
					t.Fatal(err)
				}
				before, after = "parked", "waiting"
			}
			var targetID int64
			if err = db.sql.QueryRowContext(ctx, `SELECT rowid FROM implement_stories WHERE parent=? AND generation=1 AND story_key='one'`, session.TaskRef).Scan(&targetID); err != nil {
				t.Fatal(err)
			}
			op := typedRowApproval(t, db, session, kind, fmt.Sprintf("story:%d", targetID), before, after)
			step, err := db.ApplyDoctorTypedRows(ctx, op.ID, 1, kind)
			if err != nil || step.State != "verified" || step.BackupReference == "" || step.ObservedAfter != after {
				t.Fatal(step, err)
			}
			got, err := db.GetImplementStory(ctx, session.TaskRef, 1, "one")
			if err != nil || got.State != after || got.Revision != row.Revision+1 {
				t.Fatal(got, err)
			}
		})
	}
}

func TestDoctorTypedCommentCannotConsumeWithoutReplyAdapter(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 645)
	ctx := t.Context()
	if _, _, err := db.BootstrapWatermark(ctx, session.ID, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordComment(ctx, Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 10, Body: "stuck", CreatedAt: graphTestTime(), Classification: ClassificationInstruction}); err != nil {
		t.Fatal(err)
	}
	op := typedRowApproval(t, db, session, "consume_comment", "comment:11", "10", "11")
	if _, err := db.ApplyDoctorTypedRows(ctx, op.ID, 1, "consume_comment"); err == nil {
		t.Fatal("comment consumed without visible reply")
	}
	watermark, ok, err := db.GetWatermark(ctx, session.ID)
	if err != nil || !ok || watermark.LastCommentID != 10 {
		t.Fatal(watermark, ok, err)
	}
	steps, err := db.DoctorRepairSteps(ctx, op.ID)
	if err != nil || steps[0].State != "pending" || steps[0].BackupReference != "" {
		t.Fatal(steps, err)
	}
}

func TestDoctorTypedRowsRequirePresentedOwnerApproval(t *testing.T) {
	for _, principal := range []string{"local-owner", "third-party"} {
		t.Run(principal, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 646)
			schedule := typedSchedule(t, db, session, true)
			presentation := typedRowProposal(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
			if principal == "local-owner" {
				presentation = "unpresented"
			}
			if _, err := db.ReserveDoctorRepair(t.Context(), DoctorApproval{ID: "unapproved", SourceID: "apply", Principal: principal, TaskRef: session.TaskRef, PresentationID: presentation}); err == nil {
				t.Fatal("unapproved repair reserved")
			}
			if _, err := db.ApplyDoctorTypedRows(t.Context(), "unapproved", 1, "requeue_schedule"); err == nil {
				t.Fatal("unapproved tool executed")
			}
			got, err := db.GetSchedule(t.Context(), schedule.ID)
			if err != nil || got.Status != ScheduleRunning {
				t.Fatal(got, err)
			}
			var journals int
			if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM doctor_repair_steps`).Scan(&journals); err != nil || journals != 0 {
				t.Fatal("unapproved journal exists", journals, err)
			}
		})
	}
}

func TestDoctorTypedReleaseCancelsClaimedWaitAndUnallocatedDispatch(t *testing.T) {
	db, instance, _ := closureWaitFixture(t, "validate", "claimed")
	ctx := t.Context()
	session, err := db.GetSession(ctx, instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE sessions SET state='running' WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	res, err := db.sql.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at) VALUES(?,?,?,?,'pending',?)`, instance.ID, session.ID, instance.CurrentNode, FormatTime(graphTestTime()), FormatTime(graphTestTime()))
	if err != nil {
		t.Fatal(err)
	}
	dispatchID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	op := typedRowApproval(t, db, session, "release_instance", fmt.Sprintf("graph:%d", instance.ID), "running", "failed")
	step, err := db.ApplyDoctorTypedRows(ctx, op.ID, 1, "release_instance")
	if err != nil || step.State != "verified" || step.BackupReference == "" {
		t.Fatal(step, err)
	}
	var status, waitState, dispatchStatus, reason string
	var finished *string
	if err = db.sql.QueryRowContext(ctx, `SELECT status FROM graph_instances WHERE id=?`, instance.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatal(status, err)
	}
	if err = db.sql.QueryRowContext(ctx, `SELECT state FROM graph_owner_waits WHERE delivery_key='question'`).Scan(&waitState); err != nil || waitState != "cancelled" {
		t.Fatal(waitState, err)
	}
	if err = db.sql.QueryRowContext(ctx, `SELECT status,finished_at,cancellation_reason FROM graph_dispatches WHERE id=?`, dispatchID).Scan(&dispatchStatus, &finished, &reason); err != nil || dispatchStatus != "cancelled" || finished == nil || reason == "" {
		t.Fatal(dispatchStatus, finished, reason, err)
	}
	var backup []byte
	if err = db.sql.QueryRowContext(ctx, `SELECT backup FROM doctor_repair_steps WHERE repair_id=? AND number=1`, op.ID).Scan(&backup); err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Status     string `json:"status"`
		OwnerWaits []struct {
			DeliveryKey string `json:"delivery_key"`
			State       string `json:"state"`
		} `json:"owner_waits"`
		Dispatches []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"dispatches"`
	}
	if err = json.Unmarshal(backup, &saved); err != nil || saved.Status != "running" || len(saved.OwnerWaits) != 1 || saved.OwnerWaits[0].DeliveryKey != "question" || saved.OwnerWaits[0].State != "claimed" || len(saved.Dispatches) != 1 || saved.Dispatches[0].ID != dispatchID || saved.Dispatches[0].Status != "pending" {
		t.Fatalf("backup %s: %v", backup, err)
	}
}

func TestDoctorTypedReleaseRefusesLiveAttemptOrWorker(t *testing.T) {
	for _, live := range []string{"attempt", "run", "lock"} {
		t.Run(live, func(t *testing.T) {
			var db *DB
			var instance GraphInstance
			if live == "attempt" {
				db = newTestDB(t)
				instance = graphTestInstance(t, db)
				graphTestAttempt(t, db, instance, 3)
			} else {
				db, instance, _ = closureWaitFixture(t, "validate", "claimed")
			}
			session, err := db.GetSession(t.Context(), instance.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			switch live {
			case "run":
				run := testRun(t, db, session.ID, 1)
				if _, err = db.sql.ExecContext(t.Context(), `UPDATE runs SET status='running' WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
			case "lock":
				if _, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET lock_owner='live-turn' WHERE id=?`, session.ID); err != nil {
					t.Fatal(err)
				}
			}
			op := typedRowApproval(t, db, session, "release_instance", fmt.Sprintf("graph:%d", instance.ID), "running", "failed")
			if _, err = db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, "release_instance"); err == nil {
				t.Fatal("live graph released")
			}
			var status string
			if err = db.sql.QueryRowContext(t.Context(), `SELECT status FROM graph_instances WHERE id=?`, instance.ID).Scan(&status); err != nil || status != "running" {
				t.Fatal(status, err)
			}
			if live != "attempt" {
				var waitState string
				if err = db.sql.QueryRowContext(t.Context(), `SELECT state FROM graph_owner_waits WHERE delivery_key='question'`).Scan(&waitState); err != nil || waitState != "claimed" {
					t.Fatal(waitState, err)
				}
			}
			steps, err := db.DoctorRepairSteps(t.Context(), op.ID)
			if err != nil || steps[0].State != "pending" || steps[0].BackupReference != "" {
				t.Fatal(steps, err)
			}
		})
	}
}

func TestDoctorTypedConsumeUsesConfirmedReplyAndPreservesBackup(t *testing.T) {
	for _, mode := range []string{"confirmed", "missing", "unconfirmed", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 647)
			ctx := t.Context()
			if _, _, err := db.BootstrapWatermark(ctx, session.ID, 10); err != nil {
				t.Fatal(err)
			}
			if _, err := db.RecordComment(ctx, Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 10, Body: "stuck", CreatedAt: graphTestTime(), Classification: ClassificationInstruction}); err != nil {
				t.Fatal(err)
			}
			key := "consume-reply"
			if mode != "missing" {
				target := session
				if mode == "foreign" {
					target = testSession(t, db, 648)
				}
				if _, err := db.EnsureReportDelivery(ctx, ReportDelivery{Key: key, Version: 1, SessionID: target.ID, TriggerID: 11, Task: target.TaskRef, Destination: target.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "Owner approved consumption", Marker: "marker"}); err != nil {
					t.Fatal(err)
				}
				if mode != "unconfirmed" {
					if _, err := db.MarkReportDeliverySent(ctx, key); err != nil {
						t.Fatal(err)
					}
					if _, err := db.ConfirmReportDelivery(ctx, key, 100); err != nil {
						t.Fatal(err)
					}
				}
			}
			op := typedRowApproval(t, db, session, "consume_comment", "comment:11", "10", "11")
			step, err := db.ApplyDoctorConsumeComment(ctx, op.ID, 1, key)
			if mode == "confirmed" {
				if err != nil || step.State != "verified" || step.BackupReference == "" || step.ObservedAfter != "11" {
					t.Fatal(step, err)
				}
				watermark, ok, err := db.GetWatermark(ctx, session.ID)
				if err != nil || !ok || watermark.LastCommentID != 11 {
					t.Fatal(watermark, ok, err)
				}
				delivery, err := db.GetReportDelivery(ctx, key)
				if err != nil || !delivery.Completed {
					t.Fatal(delivery, err)
				}
			} else {
				if err == nil {
					t.Fatal("unconfirmed or foreign reply consumed comment")
				}
				watermark, ok, err := db.GetWatermark(ctx, session.ID)
				if err != nil || !ok || watermark.LastCommentID != 10 {
					t.Fatal(watermark, ok, err)
				}
				steps, err := db.DoctorRepairSteps(ctx, op.ID)
				if err != nil || steps[0].State != "pending" || steps[0].BackupReference != "" {
					t.Fatal(steps, err)
				}
			}
		})
	}
}

func TestDoctorTypedRowsRespectOrderingAndRetainEarlierBackup(t *testing.T) {
	for _, mode := range []string{"first-refused", "second-refused"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 650)
			ctx := t.Context()
			first := typedSchedule(t, db, session, true)
			second, _, err := db.CreateOrGetSchedule(ctx, CreateScheduleParams{SessionID: session.ID, SourceCommentID: 8, OwnerHandle: "owner", Payload: "second", DueAt: graphTestTime()})
			if err != nil {
				t.Fatal(err)
			}
			_, presentation := retainedRowProposal(t, db, session, []byte(fmt.Sprintf(`[{"number":1,"target":"schedule:%d","action":{"kind":"requeue_schedule","before":"running","after":"pending"}},{"number":2,"target":"schedule:%d","action":{"kind":"cancel_schedule","before":"pending","after":"failed"}}]`, first.ID, second.ID)))
			op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "ordered", SourceID: "apply", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.AcknowledgeDoctorExclusion(ctx, op.ID, "daemon"); err != nil {
				t.Fatal(err)
			}
			var earlier DoctorRepairStep
			if mode == "first-refused" {
				if _, err = db.ApplyDoctorTypedRows(ctx, op.ID, 1, "cancel_schedule"); err == nil {
					t.Fatal("wrong first tool accepted")
				}
				if _, err = db.ApplyDoctorTypedRows(ctx, op.ID, 2, "cancel_schedule"); err == nil {
					t.Fatal("second tool crossed unfinished first step")
				}
			} else {
				earlier, err = db.ApplyDoctorTypedRows(ctx, op.ID, 1, "requeue_schedule")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.ApplyDoctorTypedRows(ctx, op.ID, 2, "requeue_schedule"); err == nil {
					t.Fatal("wrong second tool accepted")
				}
			}
			steps, err := db.DoctorRepairSteps(ctx, op.ID)
			if err != nil || len(steps) != 2 {
				t.Fatal(steps, err)
			}
			if steps[1].State != "pending" || steps[1].BackupReference != "" {
				t.Fatal("refused step changed", steps)
			}
			if mode == "first-refused" {
				if steps[0].State != "pending" || steps[0].BackupReference != "" {
					t.Fatal(steps)
				}
			} else {
				if steps[0].State != "verified" || steps[0].BackupDigest != earlier.BackupDigest || steps[0].BackupReference != earlier.BackupReference {
					t.Fatal("earlier backup lost", steps)
				}
			}
			got, err := db.GetSchedule(ctx, second.ID)
			if err != nil || got.Status != SchedulePending {
				t.Fatal("second schedule changed", got, err)
			}
		})
	}
}

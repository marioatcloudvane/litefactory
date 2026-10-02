package store

import (
	"errors"
	"fmt"
	"testing"
)

func TestDoctorRepairRunLockAndStartupExclusion(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	selected := testSession(t, db, 406)
	sibling := testSession(t, db, 407)
	run := testRun(t, db, selected.ID, 101)
	other := testRun(t, db, sibling.ID, 102)
	if ok, err := db.AcquireSessionLock(ctx, selected.ID, "old-instance"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	proposal := []byte(fmt.Sprintf(`[{"number":1,"target":"run:%d","action":{"kind":"run_interrupt","before":"running","after":"failed"}},{"number":2,"target":"session_lock:%d","action":{"kind":"session_unlock","before":"old-instance","after":""}}]`, run.ID, selected.ID))
	_, presentation := retainedRowProposal(t, db, selected, proposal)
	op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "repair", SourceID: "approve", Principal: "local-owner", TaskRef: selected.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReconcileOrphans(ctx, "replacement"); err != nil {
		t.Fatal(err)
	}
	untouched, err := db.GetRun(ctx, run.ID)
	if err != nil || untouched.Status != RunRunning {
		t.Fatal("startup changed held run", untouched, err)
	}
	recovered, err := db.GetRun(ctx, other.ID)
	if err != nil || recovered.Status != RunFailed {
		t.Fatal("startup failed unrelated recovery", recovered, err)
	}
	if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded}); err == nil {
		t.Fatal("workflow result crossed hold")
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, op.ID, "replacement"); err != nil {
		t.Fatal(err)
	}
	if err = db.BackupDoctorRows(ctx, op.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorRows(ctx, op.ID, 1); err == nil {
		t.Fatal("write allowed before all backups")
	}
	if err = db.BackupDoctorRows(ctx, op.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorRows(ctx, op.ID, 2); err == nil {
		t.Fatal("out of order unlock allowed")
	}
	for _, number := range []int{1, 2} {
		if err = db.ApplyDoctorRows(ctx, op.ID, number); err != nil {
			t.Fatal(number, err)
		}
		if err = db.VerifyDoctorRows(ctx, op.ID, number); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.GetRun(ctx, run.ID)
	if err != nil || got.Status != RunFailed || got.Error == "" || got.FinishedAt.IsZero() {
		t.Fatal("run terminal metadata missing", got, err)
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE session_id=?`, selected.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("history lost", count, err)
	}
}

func TestDoctorRepairCatalogRejectsForgedTransitions(t *testing.T) {
	for _, tc := range []struct{ kind, target, before, after string }{
		{"run_interrupt", "run:1", "running", "succeeded"},
		{"watermark_rewind", "watermark:1", "10", "20"},
		{"report_completion", "report:1", "unconfirmed", "completed"},
		{"graph_fail", "graph:1", "running", "succeeded"},
		{"session_unlock", "session_lock:1", "", "foreign"},
		{"graph_attempt_cancel", "graph_attempt:1", "succeeded", "cancelled"},
	} {
		if DoctorRowRepairTarget(tc.kind, tc.target, tc.before, tc.after) {
			t.Errorf("unsafe transition accepted %+v", tc)
		}
	}
	db := newTestDB(t)
	selected := testSession(t, db, 406)
	sibling := testSession(t, db, 407)
	run := testRun(t, db, sibling.ID, 101)
	_, presentation := retainedRowProposal(t, db, selected, []byte(fmt.Sprintf(`[{"number":1,"target":"run:%d","action":{"kind":"run_interrupt","before":"running","after":"failed"}}]`, run.ID)))
	if _, err := db.ReserveDoctorRepair(t.Context(), DoctorApproval{ID: "foreign", SourceID: "approve", Principal: "local-owner", TaskRef: selected.TaskRef, PresentationID: presentation}); err == nil {
		t.Fatal("foreign row accepted")
	}
	if held, err := db.DoctorRepairHeld(t.Context(), selected.TaskRef); err != nil || held {
		t.Fatal("failed approval retained hold", held, err)
	}
}

func TestDoctorRepairGraphCancellationPreservesHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	instance := graphTestInstance(t, db)
	attempt := graphTestAttempt(t, db, instance, 3)
	session, err := db.GetSession(ctx, instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	proposal := []byte(fmt.Sprintf(`[{"number":1,"target":"graph_attempt:%d","action":{"kind":"graph_attempt_cancel","before":"running","after":"cancelled"}},{"number":2,"target":"graph:%d","action":{"kind":"graph_fail","before":"running","after":"failed"}}]`, attempt.ID, instance.ID))
	_, presentation := retainedRowProposal(t, db, session, proposal)
	op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "graph-repair", SourceID: "approve", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReconcileWorkflowGraphs(ctx, "implement", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = db.sql.QueryRowContext(ctx, `SELECT status FROM graph_node_attempts WHERE id=?`, attempt.ID).Scan(&state); err != nil || state != "running" {
		t.Fatal("held graph recovered", state, err)
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, op.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 2} {
		if err = db.BackupDoctorRows(ctx, op.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 2} {
		if err = db.ApplyDoctorRows(ctx, op.ID, n); err != nil {
			t.Fatal(n, err)
		}
	}
	if err = db.sql.QueryRowContext(ctx, `SELECT status FROM graph_instances WHERE id=?`, instance.ID).Scan(&state); err != nil || state != "failed" {
		t.Fatal(state, err)
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts WHERE id=? AND status='cancelled'`, attempt.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("attempt history lost", count, err)
	}
}

func TestDoctorRepairGitHubApprovalIsAtomicAndCannotRebind(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	repo := Repo{ID: session.RepoID}
	proposalID, _ := retainedPollProposal(t, db, session)
	input := doctorInboxInput(repo, 406, 10, "diagnose")
	input.TaskRef = session.TaskRef
	_, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	command, err := db.ClaimDoctorCommand(ctx, "", false, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteDoctorCommand(ctx, command.ID, "proposal", 0); err != nil {
		t.Fatal(err)
	}
	// Model retained proposal attribution, then use the real multipart publisher.
	if _, err = db.sql.ExecContext(ctx, `UPDATE doctor_inbox SET proposal_id=? WHERE id=?`, proposalID, command.ID); err != nil {
		t.Fatal(err)
	}
	command.ProposalID = proposalID
	command.ResultBody = "proposal"
	if err = db.EnsureDoctorDeliveries(ctx, command, []string{"part one", "part two"}, 99); err != nil {
		t.Fatal(err)
	}
	approval := doctorInboxInput(repo, 406, 200, "apply")
	approval.TaskRef = session.TaskRef
	approval.RepairApprovalID = "approved-repair"
	if _, err = db.AdmitDoctorCommand(ctx, approval); err == nil {
		t.Fatal("incomplete presentation approved")
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM doctor_inbox WHERE comment_id=200`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed approval not atomic", count, err)
	}
	deliveries, err := db.PendingDoctorDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range deliveries {
		if _, err = db.MarkReportDeliverySent(ctx, d.Key); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ConfirmReportDelivery(ctx, d.Key, int64(100+i)); err != nil {
			t.Fatal(err)
		}
		if err = db.CompleteDoctorDelivery(ctx, d.Key); err != nil {
			t.Fatal(err)
		}
	}
	admitted, err := db.AdmitDoctorCommand(ctx, approval)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := db.ReadDoctorRepair(ctx, session.TaskRef)
	if err != nil || operation.ProposalID != proposalID {
		t.Fatal(operation, err)
	}
	approval.RepairApprovalID = "another-operation"
	replay, err := db.AdmitDoctorCommand(ctx, approval)
	if err != nil || replay.ID != admitted.ID {
		t.Fatal("replay changed immutable approval", replay, err)
	}
	if _, err = db.ClaimDoctorCommand(ctx, "", true, "daemon"); !errors.Is(err, ErrNotFound) {
		t.Fatal("apply occupied short control path", err)
	}
	claimed, err := db.ClaimDoctorCommand(ctx, "", false, "daemon")
	if err != nil || claimed.ID != admitted.ID {
		t.Fatal("reserved worker did not claim apply", claimed, err)
	}
	if err = db.ClaimDoctorRepairDispatch(ctx, operation.ID, operation.SourceID); err != nil {
		t.Fatal(err)
	}
	if err = db.ClaimDoctorRepairDispatch(ctx, operation.ID, operation.SourceID); err == nil {
		t.Fatal("writing agent replay permitted")
	}

	if err = db.AcknowledgeDoctorExclusion(ctx, operation.ID, "old-instance"); err != nil {
		t.Fatal(err)
	}
	if err = db.BackupDoctorRows(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorRows(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.CommitDoctorRestart(ctx, operation.ID, "recovery-token", "Retained outcome"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimDoctorRestart(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorRestart(ctx, operation.ID, "recovery-token", "new-instance"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = db.EnsureDoctorRepairOutcome(ctx, operation.ID, []string{"changes and backups", "restart and unresolved findings"}, 99); err != nil {
			t.Fatal(err)
		}
	}
	outcomeDeliveries, err := db.PendingDoctorDeliveries(ctx)
	if err != nil || len(outcomeDeliveries) != 2 {
		t.Fatal("outcome replay duplicated delivery", outcomeDeliveries, err)
	}
	for _, delivery := range outcomeDeliveries {
		if delivery.Destination != session.TaskRef || delivery.TriggerID != approval.CommentID {
			t.Fatal("outcome destination drift", delivery)
		}
	}
}

func TestDoctorRepairConfirmedReportDoesNotConsumeSource(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	run := testRun(t, db, session.ID, 55)
	if _, err := db.FinishRun(ctx, run.ID, RunOutcome{Status: RunFailed, Error: "original failure"}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(session, run)
	if _, err := db.EnsureReportDelivery(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkReportDeliverySent(ctx, intent.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmReportDelivery(ctx, intent.Key, 500); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdvanceWatermark(ctx, session.ID, 54); err != nil {
		t.Fatal(err)
	}
	proposal := []byte(fmt.Sprintf(`[{"number":1,"target":"report:%s","action":{"kind":"report_completion","before":"confirmed","after":"completed"}},{"number":2,"target":"watermark:%d","action":{"kind":"watermark_rewind","before":"54","after":"0"}}]`, intent.Key, session.ID))
	_, presentation := retainedRowProposal(t, db, session, proposal)
	op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "report-repair", SourceID: "approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, op.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 2} {
		if err = db.BackupDoctorRows(ctx, op.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.ApplyDoctorRows(ctx, op.ID, 1); err != nil {
		t.Fatal(err)
	}
	watermark, _, err := db.GetWatermark(ctx, session.ID)
	if err != nil || watermark.LastCommentID != 54 {
		t.Fatal("report repair consumed source", watermark, err)
	}
	unchanged, err := db.GetRun(ctx, run.ID)
	if err != nil || unchanged.Error != "original failure" || unchanged.Status != RunFailed {
		t.Fatal("report repair changed run history", unchanged, err)
	}
	if err = db.ApplyDoctorRows(ctx, op.ID, 2); err != nil {
		t.Fatal(err)
	}
	watermark, _, err = db.GetWatermark(ctx, session.ID)
	if err != nil || watermark.LastCommentID != 0 {
		t.Fatal("explicit rewind failed", watermark, err)
	}
}

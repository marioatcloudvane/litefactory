package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestAlertTaskSourcesAuthoritativeWaitAndProgress(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	g := graphTestInstance(t, db)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	old := at.Add(-3 * time.Hour)
	if _, err := db.sql.ExecContext(ctx, `UPDATE sessions SET state='running',created_at=?,needs_poll=1 WHERE id=?`, FormatTime(old), g.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGraphInstanceStatus(ctx, g.ID, "running", time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	rows, complete, err := db.AlertTaskSources(ctx, at)
	if err != nil || !complete || len(rows) != 1 || rows[0].Waiting || rows[0].Eligible || !rows[0].ProgressAt.Equal(old) {
		t.Fatalf("initial sources=%+v complete=%t error=%v", rows, complete, err)
	}
	transitionAt := at.Add(-time.Hour)
	if _, err = db.RecordGraphTransition(ctx, GraphTransition{InstanceID: g.ID, FromNode: "work", ToNode: "review", OnOutcome: "success", At: transitionAt}); err != nil {
		t.Fatal(err)
	}
	rows, _, err = db.AlertTaskSources(ctx, at)
	if err != nil || !rows[0].ProgressAt.Equal(transitionAt) {
		t.Fatalf("stage progress=%+v %v", rows, err)
	}
	run := testRun(t, db, g.SessionID, 55)
	runAt := at.Add(-30 * time.Minute)
	if _, err = db.sql.ExecContext(ctx, `UPDATE runs SET started_at=?,finished_at=NULL WHERE id=?`, FormatTime(runAt), run.ID); err != nil {
		t.Fatal(err)
	}
	rows, _, err = db.AlertTaskSources(ctx, at)
	if err != nil || !rows[0].ProgressAt.Equal(runAt) {
		t.Fatalf("run start progress=%+v %v", rows, err)
	}
	finishAt := at.Add(-20 * time.Minute)
	if _, err = db.sql.ExecContext(ctx, `UPDATE runs SET finished_at=? WHERE id=?`, FormatTime(finishAt), run.ID); err != nil {
		t.Fatal(err)
	}
	rows, _, err = db.AlertTaskSources(ctx, at)
	if err != nil || !rows[0].ProgressAt.Equal(finishAt) {
		t.Fatalf("run finish progress=%+v %v", rows, err)
	}
	attempt := graphTestAttempt(t, db, g, 3)
	resultAt := at.Add(-10 * time.Minute)
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'json','{}',?)`, attempt.ID, FormatTime(resultAt)); err != nil {
		t.Fatal(err)
	}
	rows, _, err = db.AlertTaskSources(ctx, at)
	if err != nil || !rows[0].ProgressAt.Equal(resultAt) {
		t.Fatalf("saved result progress=%+v %v", rows, err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate) VALUES('unpublished-wait',?,?, 'question','question','{}')`, g.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state) VALUES('unpublished-wait',?,?,'waiting')`, g.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	rows, _, err = db.AlertTaskSources(ctx, at)
	if err != nil || !rows[0].Waiting {
		t.Fatalf("unpublished owner wait ignored=%+v %v", rows, err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE graph_owner_waits SET state='cancelled' WHERE delivery_key='unpublished-wait'`); err != nil {
		t.Fatal(err)
	}
	if err = db.SetGraphInstanceStatus(ctx, g.ID, "running", at.Add(time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	rows, _, err = db.AlertTaskSources(ctx, at)
	if err != nil || !rows[0].Waiting {
		t.Fatalf("planned wait ignored=%+v %v", rows, err)
	}
	if err = db.SetGraphInstanceStatus(ctx, g.ID, GraphSucceeded, time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE sessions SET state='idle',needs_poll=0 WHERE id=?`, g.SessionID); err != nil {
		t.Fatal(err)
	}
	settled, err := db.AlertTaskTerminal(ctx, rows[0].Task)
	if err != nil || !settled {
		t.Fatalf("terminal source=%t %v", settled, err)
	}
}

func TestAlertReportOriginalUncertaintyAndSettlement(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	s := testSession(t, db, 1335)
	run := testRun(t, db, s.ID, 55)
	if _, err := db.FinishRun(ctx, run.ID, RunOutcome{Status: RunFailed}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(s, run)
	if _, err := db.EnsureReportDelivery(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.MarkReportDeliverySent(ctx, intent.Key); err != nil || !claimed {
		t.Fatalf("claim=%t %v", claimed, err)
	}
	var first string
	var approximate bool
	if err := db.sql.QueryRowContext(ctx, `SELECT first_at,approximate FROM alert_report_uncertainty WHERE delivery_key=?`, intent.Key).Scan(&first, &approximate); err != nil || approximate {
		t.Fatalf("original boundary=%q approximate=%t %v", first, approximate, err)
	}
	firstAt, err := ParseTime(first)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.AlertReportUncertainSince(ctx, intent.Key, firstAt.Add(time.Hour))
	if err != nil || !got.Equal(firstAt) {
		t.Fatalf("first boundary reset=%s %v", got, err)
	}
	reports, complete, err := db.AlertReportSources(ctx)
	if err != nil || !complete || len(reports) != 1 || reports[0].Key != intent.Key {
		t.Fatalf("uncertain report=%+v %t %v", reports, complete, err)
	}
	settled, err := db.AlertReportSettled(ctx, intent.Key)
	if err != nil || settled {
		t.Fatalf("uncertain falsely settled=%t %v", settled, err)
	}
	if _, err = db.ConfirmReportDelivery(ctx, intent.Key, 500); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertReportSettled(ctx, intent.Key)
	if err != nil || !settled {
		t.Fatalf("confirmed source not settled=%t %v", settled, err)
	}
}

func TestAlertMigrationPreservesUncertainReportsWithoutHistoricalSend(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "before.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "alert_ledger"))
	session := testSession(t, db, 1335)
	run := testRun(t, db, session.ID, 55)
	if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunFailed}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(session, run)
	if _, err = db.EnsureReportDelivery(ctx, intent); err != nil {
		t.Fatal(err)
	}
	// This row represents an already-sent report before the alert timestamp existed.
	if _, err = db.sql.ExecContext(ctx, `UPDATE report_deliveries SET possibly_sent=1 WHERE delivery_key=?`, intent.Key); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || len(ledger.Notifications) != 0 {
		t.Fatalf("historical alerts seeded=%+v %v", ledger, err)
	}
	reports, _, err := db.AlertReportSources(ctx)
	if err != nil || len(reports) != 1 || reports[0].Body != intent.Body || !reports[0].PossiblySent || reports[0].CommentID != 0 {
		t.Fatalf("report changed=%+v %v", reports, err)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	first, err := db.AlertReportUncertainSince(ctx, intent.Key, at)
	if err != nil || !first.Equal(at) {
		t.Fatalf("fallback=%s %v", first, err)
	}
	var approximate bool
	if err = db.sql.QueryRowContext(ctx, `SELECT approximate FROM alert_report_uncertainty WHERE delivery_key=?`, intent.Key).Scan(&approximate); err != nil || !approximate {
		t.Fatalf("fallback provenance=%t %v", approximate, err)
	}
}

func TestAlertQueueAndDispatchRequireOriginalSettlement(t *testing.T) {
	t.Parallel()
	db, g := graphDispatchFixture(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	key := fmt.Sprintf("%d:55", g.SessionID)
	settled, err := db.AlertTaskSettled(ctx, "queue", key)
	if err != nil || settled {
		t.Fatalf("absent watermark=%t %v", settled, err)
	}
	if _, _, err = db.BootstrapWatermark(ctx, g.SessionID, 54); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertTaskSettled(ctx, "queue", key)
	if err != nil || settled {
		t.Fatalf("unconsumed queue=%t %v", settled, err)
	}
	if _, err = db.AdvanceWatermark(ctx, g.SessionID, 55); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertTaskSettled(ctx, "queue", key)
	if err != nil || !settled {
		t.Fatalf("consumed queue=%t %v", settled, err)
	}
	dispatch, err := db.ParkGraphInstance(ctx, g.ID, g.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertTaskSettled(ctx, "dispatch", fmt.Sprint(dispatch.ID))
	if err != nil || settled {
		t.Fatalf("pending dispatch=%t %v", settled, err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled' WHERE id=?`, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertTaskSettled(ctx, "dispatch", fmt.Sprint(dispatch.ID))
	if err != nil || !settled {
		t.Fatalf("cancelled dispatch=%t %v", settled, err)
	}
}

func TestAlertTaskSourcesUnpublishedPlanWaitIncludesChild(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "repo")
	parent, err := db.GetOrCreateSession(ctx, repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.GetOrCreateSession(ctx, repo, 20)
	if err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "story", Stage: "story", Candidate: "candidate", Draft: planbrief.PlanDraft{Key: "story"}}}}
	if err = state.OpenPlanWait("story", "unpublished-story-wait", "review", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number) VALUES(?,'story',1,1,20)`, parent.TaskRef); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	rows, complete, err := db.AlertTaskSources(ctx, at)
	if err != nil || !complete {
		t.Fatalf("plan scan=%+v complete=%t %v", rows, complete, err)
	}
	waiting := map[string]bool{}
	for _, row := range rows {
		waiting[row.Task] = row.Waiting && !row.Eligible
	}
	if !waiting[parent.TaskRef] || !waiting[child.TaskRef] {
		t.Fatalf("unpublished parent/child owner waits missing: %+v", rows)
	}
	// Unknown or over-budget Plan state cannot establish a healthy pool.
	if _, err = db.sql.ExecContext(ctx, `UPDATE plan_workflow_states SET state_json='{' WHERE parent=?`, parent.TaskRef); err != nil {
		t.Fatal(err)
	}
	_, complete, err = db.AlertTaskSources(ctx, at)
	if err != nil || complete {
		t.Fatalf("malformed plan accepted complete=%t %v", complete, err)
	}
}

func TestAlertTaskSourcesEligibilityRequiresActionableDueWork(t *testing.T) {
	t.Parallel()
	db, g := graphDispatchFixture(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.SetGraphInstanceStatus(ctx, g.ID, GraphRunning, time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionNeedsPoll(ctx, g.SessionID); err != nil {
		t.Fatal(err)
	}
	read := func() AlertTaskSource {
		t.Helper()
		rows, complete, err := db.AlertTaskSources(ctx, at)
		if err != nil || !complete || len(rows) != 1 {
			t.Fatalf("sources=%+v %t %v", rows, complete, err)
		}
		return rows[0]
	}
	if source := read(); source.Eligible {
		t.Fatal("needs_poll alone claimed runnable work")
	}
	dispatch, err := db.ParkGraphInstance(ctx, g.ID, g.CurrentNode, at.Add(time.Hour), at)
	if err != nil {
		t.Fatal(err)
	}
	if source := read(); source.Eligible {
		t.Fatal("future dispatch claimed runnable work")
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE graph_dispatches SET due_at=? WHERE id=?`, FormatTime(at), dispatch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(at), g.ID); err != nil {
		t.Fatal(err)
	}
	if source := read(); !source.Eligible {
		t.Fatalf("due dispatch ineligible: %+v", source)
	}
	if err = db.SetSessionState(ctx, g.SessionID, SessionRunning); err != nil {
		t.Fatal(err)
	}
	if source := read(); source.Eligible {
		t.Fatal("running session counted as pool waiter")
	}
	if err = db.SetSessionState(ctx, g.SessionID, SessionIdle); err != nil {
		t.Fatal(err)
	}
	if err = db.CancelGraphDispatch(ctx, dispatch.ID, "settled", at); err != nil {
		t.Fatal(err)
	}
	schedule, _, err := db.CreateOrGetSchedule(ctx, CreateScheduleParams{SessionID: g.SessionID, SourceCommentID: 99, OwnerHandle: "owner", Payload: "work", DueAt: at.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if source := read(); source.Eligible || !source.Waiting {
		t.Fatalf("future schedule must be planned wait: %+v", source)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE schedules SET due_at=? WHERE id=?`, FormatTime(at), schedule.ID); err != nil {
		t.Fatal(err)
	}
	if source := read(); !source.Eligible || source.Waiting {
		t.Fatalf("due schedule missing eligibility: %+v", source)
	}
	settled, err := db.AlertTaskSettled(ctx, "schedule", fmt.Sprint(schedule.ID))
	if err != nil || settled {
		t.Fatalf("pending schedule falsely settled %t %v", settled, err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE schedules SET status='succeeded' WHERE id=?`, schedule.ID); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertTaskSettled(ctx, "schedule", fmt.Sprint(schedule.ID))
	if err != nil || !settled {
		t.Fatalf("completed schedule not settled %t %v", settled, err)
	}
}

func TestAlertIssueBodyAndSyntheticQueueSettlement(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 1335)
	ctx := t.Context()
	if _, _, err := db.BootstrapTicketWatermark(ctx, session.ID, 0); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%d:0", session.ID)
	settled, err := db.AlertTaskSettled(ctx, "queue", key)
	if err != nil || settled {
		t.Fatalf("pending issue body falsely settled %t %v", settled, err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE watermarks SET ticket_body_pending=0 WHERE session_id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertTaskSettled(ctx, "queue", key)
	if err != nil || !settled {
		t.Fatalf("consumed issue body not settled %t %v", settled, err)
	}
	for _, source := range []int64{-1, GraphDispatch{ID: 1}.TriggerID(), Schedule{ID: 1}.TriggerID()} {
		settled, err = db.AlertTaskSettled(ctx, "queue", fmt.Sprintf("%d:%d", session.ID, source))
		if err != nil || settled {
			t.Fatalf("synthetic source falsely settled %d %t %v", source, settled, err)
		}
	}
}

func TestAlertReportRecoveryOnlyUncertaintyRetainsOriginalSource(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 1335)
	run := testRun(t, db, session.ID, 55)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	uncertain := ReportRecovery{Key: "source-only", SessionID: session.ID, TriggerID: 55, RunID: run.ID, Task: session.TaskRef, State: "uncertain", DeliveryKey: "original-missing", NextAt: at}
	if err := db.PutReportRecovery(ctx, uncertain); err != nil {
		t.Fatal(err)
	}
	pending := uncertain
	pending.Key = "known-unsent"
	pending.TriggerID = 56
	pending.State = "pending"
	pending.DeliveryKey = ""
	if err := db.PutReportRecovery(ctx, pending); err != nil {
		t.Fatal(err)
	}
	reports, complete, err := db.AlertReportSources(ctx)
	if err != nil || !complete || len(reports) != 1 || reports[0].Key != "recovery-source:source-only" {
		t.Fatalf("recovery sources=%+v %t %v", reports, complete, err)
	}
	key := reports[0].Key
	first, err := db.AlertReportUncertainSince(ctx, key, at)
	if err != nil || !first.Equal(at) {
		t.Fatalf("recovery timer=%s %v", first, err)
	}
	first, err = db.AlertReportUncertainSince(ctx, key, at.Add(time.Hour))
	if err != nil || !first.Equal(at) {
		t.Fatalf("recovery timer reset=%s %v", first, err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	evidence := ledger.Evidence[key]
	if !evidence.Approximate || evidence.SourceSessionID != session.ID || evidence.SourceTriggerID != 55 || evidence.SourceRunID != run.ID || evidence.SourceDeliveryKey != "original-missing" {
		t.Fatalf("lost original recovery provenance=%+v", evidence)
	}
	if err = db.DeleteReportRecovery(ctx, uncertain.Key); err != nil {
		t.Fatal(err)
	}
	settled, err := db.AlertReportSettled(ctx, key)
	if err != nil || settled {
		t.Fatalf("source disappearance falsely settled=%t %v", settled, err)
	}
	if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunFailed}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(session, run)
	intent.Key = "original-missing"
	if _, err = db.EnsureReportDelivery(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MarkReportDeliverySent(ctx, intent.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConfirmReportDelivery(ctx, intent.Key, 500); err != nil {
		t.Fatal(err)
	}
	settled, err = db.AlertReportSettled(ctx, key)
	if err != nil || !settled {
		t.Fatalf("original confirmation not settlement=%t %v", settled, err)
	}
}

func TestAlertTaskSourcesRetainedOwnerInputSurvivesIngestionWatermark(t *testing.T) {
	t.Parallel()
	db, g := graphDispatchFixture(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.SetGraphInstanceStatus(ctx, g.ID, GraphRunning, time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	recordOwnerInput(t, db, g.SessionID, 30, "please continue")
	if _, err := db.RetainOwnerInstruction(ctx, g.SessionID, 30, 42, "please continue", OwnerInputTarget{InstanceID: g.ID, Node: g.CurrentNode}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.BootstrapWatermark(ctx, g.SessionID, 30); err != nil {
		t.Fatal(err)
	}
	rows, complete, err := db.AlertTaskSources(ctx, at)
	if err != nil || !complete || len(rows) != 1 || !rows[0].Eligible {
		t.Fatalf("acknowledged but unassigned owner input lost: %+v %t %v", rows, complete, err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE owner_instruction_inputs SET target_node='later' WHERE session_id=?`, g.SessionID); err != nil {
		t.Fatal(err)
	}
	rows, complete, err = db.AlertTaskSources(ctx, at)
	if err != nil || !complete || rows[0].Eligible {
		t.Fatalf("future target falsely actionable: %+v %t %v", rows, complete, err)
	}
}

func TestAlertTaskSourcesNeedsPollAloneDoesNotMeanWork(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 1335)
	ctx := t.Context()
	if err := db.SetSessionNeedsPoll(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	at := session.CreatedAt.Add(3 * time.Hour)
	rows, complete, err := db.AlertTaskSources(ctx, at)
	if err != nil || !complete || len(rows) != 1 || rows[0].HasWork || rows[0].Eligible || rows[0].Waiting {
		t.Fatalf("discovery-only session falsely active: %+v %t %v", rows, complete, err)
	}
	if err = db.SetSessionState(ctx, session.ID, SessionRunning); err != nil {
		t.Fatal(err)
	}
	rows, complete, err = db.AlertTaskSources(ctx, at)
	if err != nil || !complete || len(rows) != 1 || !rows[0].HasWork || rows[0].Eligible {
		t.Fatalf("running session work missing: %+v %t %v", rows, complete, err)
	}
}

func TestAlertTaskSourcesDoctorRepairHoldsIncludeParentWriters(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	parent := testSession(t, db, 1335)
	child := testSession(t, db, 1336)
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number) VALUES(?,'story',1,1,?)`, parent.TaskRef, child.IssueNumber); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, session := range []Session{parent, child} {
		if _, _, err := db.CreateOrGetSchedule(ctx, CreateScheduleParams{SessionID: session.ID, SourceCommentID: 99, OwnerHandle: "owner", Payload: "work", DueAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	_, presentation := retainedPollProposal(t, db, child)
	if _, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "alert-hold", SourceID: "owner-apply", Principal: "local-owner", TaskRef: child.TaskRef, PresentationID: presentation}); err != nil {
		t.Fatal(err)
	}
	rows, complete, err := db.AlertTaskSources(ctx, at)
	if err != nil || !complete || len(rows) != 2 {
		t.Fatalf("repair-held sources=%+v %t %v", rows, complete, err)
	}
	for _, source := range rows {
		if !source.Waiting || source.Eligible {
			t.Fatalf("owner-approved repair work counted as runnable: %+v", source)
		}
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE doctor_repair_holds SET active=0 WHERE repair_id='alert-hold'`); err != nil {
		t.Fatal(err)
	}
	rows, complete, err = db.AlertTaskSources(ctx, at)
	if err != nil || !complete {
		t.Fatal(err)
	}
	for _, source := range rows {
		if source.Waiting || !source.Eligible {
			t.Fatalf("released repair hold retained: %+v", source)
		}
	}
}

func TestAlertTaskSourcesFutureQuotaContinuationDoesNotHideRunningWork(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 123)
	if err := db.SetSessionWorktree(ctx, session.ID, "/worktree"); err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 55)
	segment, err := db.GetSegment(ctx, run.SegmentID)
	if err != nil {
		t.Fatal(err)
	}
	outcome, intent := recoveryIntent(run)
	intent.Context.ResumeIdentity = segment.SessionRef
	if _, _, err = db.FinishInterruptedRun(ctx, run.ID, outcome, intent, ""); err != nil {
		t.Fatal(err)
	}
	if err = db.SetSessionState(ctx, session.ID, SessionIdle); err != nil {
		t.Fatal(err)
	}
	at := intent.DueAt.Add(-time.Minute)
	read := func() AlertTaskSource {
		t.Helper()
		rows, complete, err := db.AlertTaskSources(ctx, at)
		if err != nil || !complete || len(rows) != 1 {
			t.Fatalf("continuation sources=%+v %t %v", rows, complete, err)
		}
		return rows[0]
	}
	if source := read(); !source.Waiting || source.Eligible || !source.HasWork || source.Running {
		t.Fatalf("future quota reset not a deliberate wait: %+v", source)
	}
	if err = db.SetSessionState(ctx, session.ID, SessionRunning); err != nil {
		t.Fatal(err)
	}
	if source := read(); source.Waiting || !source.Running || !source.HasWork {
		t.Fatalf("future continuation hid running task progress: %+v", source)
	}
	if err = db.SetSessionState(ctx, session.ID, SessionIdle); err != nil {
		t.Fatal(err)
	}
	at = intent.DueAt.Add(time.Minute)
	if source := read(); source.Waiting || !source.Eligible || source.Running {
		t.Fatalf("locally valid due continuation not actionable: %+v", source)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE segments SET session_ref='replacement-session' WHERE id=?`, run.SegmentID); err != nil {
		t.Fatal(err)
	}
	if source := read(); source.Waiting || source.Eligible {
		t.Fatalf("invalidated resume identity supplied false wait or eligibility: %+v", source)
	}
}

package store

import (
	"testing"
	"time"
)

func TestArchiveRetainsDurableWorkflowResultsAndAuthority(t *testing.T) {
	db, p, r := workflowStoreFixture(t)
	attempt := admitStoreWorkflow(t, db, p)
	if err := db.FinishWorkflowAttempt(t.Context(), attempt, WorkflowFinish{State: "succeeded", Revision: 1, Result: `{"items":[{"result":"retained"}]}`, Questions: `[]`, Notice: &WorkflowNotice{Event: "terminal", Body: "complete"}}, attempt.StartedAt); err != nil {
		t.Fatal(err)
	}
	family := archiveITSeed(t, db)
	trailer := archiveITTrailer(t, db, family)
	if _, err := db.DeleteArchivedFamily(t.Context(), family, trailer, time.Now()); err != nil {
		t.Fatal(err)
	}
	job, err := db.ReadWorkflowJob(t.Context(), r.JobID)
	if err != nil || job.State != "succeeded" || job.Result != `{"items":[{"result":"retained"}]}` {
		t.Fatalf("archive removed retained job result: %+v %v", job, err)
	}
	request, err := db.WorkflowRequestByKey(t.Context(), r.OwnerID, r.OwnerGeneration, r.Key)
	if err != nil || request.ProposalID != p.ID {
		t.Fatalf("archive removed request identity: %+v %v", request, err)
	}
	if _, err = db.ReadOwnerAction(t.Context(), p.ID); err != nil {
		t.Fatalf("archive removed confirmation authority: %v", err)
	}
	attempts, err := db.WorkflowAttempts(t.Context(), r.JobID)
	if err != nil || len(attempts) != 1 || attempts[0].ID != attempt.ID {
		t.Fatalf("archive removed attempt evidence: %+v %v", attempts, err)
	}
	notices, err := db.WorkflowNotices(t.Context(), r.JobID)
	if err != nil || len(notices) != 1 {
		t.Fatalf("archive removed delivery receipt: %+v %v", notices, err)
	}
}

func TestArchiveRetainsPrivateAdvisoryAndPublicationRecovery(t *testing.T) {
	db := newTestDB(t)
	family := archiveITSeed(t, db)
	now := time.Now().UTC()
	scope := DiscussionScope{OwnerID: 42, OwnerGeneration: 1, RepositoryRef: "github:acme/api", TaskRef: family.TaskRef}
	conversation, err := db.OpenDiscussion(t.Context(), DiscussionConversation{DiscussionScope: scope, ID: "conversation", ClientID: "client", RequestKey: "open", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	admission := AdvisoryAdmission{DiscussionScope: scope, ID: "admission", ConversationID: conversation.ID, Operation: "ask", RequestKey: "request", Digest: "digest", Basis: `{"stage":"research","revision":"saved","evidence":{"findings":["retained"]}}`, Context: `[]`, Question: "Why?", GrantID: "grant", JobID: "private-job", ExecutionID: "private-execution", CreatedAt: now}
	if _, err = db.AdmitAdvisory(t.Context(), admission); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := db.ClaimWorkflowAttempt(t.Context(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	answer := `{"answer":"Saved private answer","sources":["saved evidence"]}`
	if err = db.FinishAdvisory(t.Context(), attempt, WorkflowFinish{State: "succeeded", Result: answer, Questions: `[]`}, now); err != nil {
		t.Fatal(err)
	}
	// Retained provider and result receipts share the admitted execution identity;
	// they do not depend on the task's archived run/result tables.
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO task_advisory_invocations(execution_id,fence,price_revision_id,provider,model,thinking,deadline,usage,started_at,outcome) VALUES(?,?,1,'codex','model','high',60000000000,'{}',?,'succeeded')`, admission.ExecutionID, attempt.Fence, FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO task_advisory_result_receipts(execution_id,request_id,digest,payload,created_at) VALUES(?,'result','digest',?,?)`, admission.ExecutionID, answer, FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO task_advisory_usage_events(execution_id,sequence,event) VALUES(?,0,'{"tokens":10}')`, admission.ExecutionID); err != nil {
		t.Fatal(err)
	}
	publication, err := db.ReserveDiscussionConclusion(t.Context(), DiscussionConclusion{DiscussionScope: scope, ID: "publication", ConversationID: conversation.ID, ConclusionKey: "settled", Digest: "summary", Summary: `{"conclusion":"Saved"}`, Rendered: "Frozen discussion summary", SourceJob: admission.JobID, GrantID: "publish-grant", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), scope, publication.ID, "pending", "sending", "", "", now); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), scope, publication.ID, "sending", "uncertain", "lost response", "", now); err != nil {
		t.Fatal(err)
	}
	trailer := archiveITTrailer(t, db, family)
	if _, err = db.DeleteArchivedFamily(t.Context(), family, trailer, now); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.ReadAdvisory(t.Context(), scope, admission.JobID)
	if err != nil || recovered.Job.Result != answer || recovered.Basis != admission.Basis {
		t.Fatalf("lost advisory recovery: %+v %v", recovered, err)
	}
	retained, err := db.ReadDiscussion(t.Context(), scope, conversation.ID)
	if err != nil || len(retained.Exchanges) != 1 {
		t.Fatalf("lost bounded follow-up: %+v %v", retained, err)
	}
	conclusion, err := db.ReadDiscussionConclusion(t.Context(), scope, publication.ID)
	if err != nil || conclusion.State != "uncertain" || conclusion.Rendered != publication.Rendered {
		t.Fatalf("lost send uncertainty: %+v %v", conclusion, err)
	}
	_, payload, err := db.ReadAdvisoryResult(t.Context(), AdvisoryResultBinding{ExecutionID: admission.ExecutionID, Fence: attempt.Fence}, "result")
	if err != nil || string(payload) != answer {
		t.Fatalf("lost result receipt: %s %v", payload, err)
	}
	for _, table := range []string{"task_advisory_invocations", "task_advisory_usage_events"} {
		var count int
		if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table+` WHERE execution_id=?`, admission.ExecutionID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("lost %s: %d %v", table, count, err)
		}
	}
	if remaining := archiveITCount(t, db); remaining != 0 {
		t.Fatalf("family comments were not archived: %d", remaining)
	}
}

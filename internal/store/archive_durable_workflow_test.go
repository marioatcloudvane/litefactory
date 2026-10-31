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

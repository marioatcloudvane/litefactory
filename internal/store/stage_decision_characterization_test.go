package store

import (
	"reflect"
	"testing"
)

// Revision guidance consumes the reviewed wait without approving either the
// retained diagnosis or a replacement that has not been produced yet.
func TestStageDecisionCharacterizationRevisionCannotApproveUnseenReplacement(t *testing.T) {
	db, path, instance, session, candidate, conversation := graphDiagnosisFixture(t)
	reply := parkGraphDiagnosisReview(t, db, instance, session, candidate, conversation, true)
	reply.Body = "Revise the remedy to preserve the caller's error context."
	claimed, err := db.ClaimGraphOwnerReply(t.Context(), reply)
	if err != nil {
		t.Fatal(err)
	}
	db = reopenBugStopDB(t, db, path)
	approval := reply
	approval.CommentID++
	approval.Body = "/approve-bug analysis 1"
	if _, err = db.ApproveGraphDiagnosis(t.Context(), approval, candidate.Revision); err == nil {
		t.Fatal("revision's consumed wait admitted an approval")
	}
	current, approved, err := db.CurrentGraphDiagnosis(t.Context(), instance.ID)
	if err != nil || approved != nil || !reflect.DeepEqual(current, candidate) {
		t.Fatalf("revision changed diagnosis or approval before replacement: %+v %+v %v", current, approved, err)
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), instance.ID)
	if err != nil || wait.State != "claimed" || wait.ReplyID != claimed.ReplyID || wait.ReplyBody != reply.Body {
		t.Fatalf("approval refusal changed admitted revision: %+v %v", wait, err)
	}
	guidance, err := db.GraphDiagnosisGuidance(t.Context(), instance.ID)
	if err != nil || len(guidance) != 1 || guidance[0].ReplyBody != reply.Body {
		t.Fatalf("revision guidance lost after reconnect: %+v %v", guidance, err)
	}
}

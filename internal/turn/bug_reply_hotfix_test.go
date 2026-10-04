package turn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// A refused owner comment is marked for a reply and consumption; shutdown and
// the between-questions refusal keep their own handling.
func TestBugReplyNotAppliedMarksRefusals(t *testing.T) {
	if e := bugReplyNotApplied(errors.New("approval revision is invalid")); !errors.Is(e, ErrBugReplyNotApplied) {
		t.Fatalf("refusal not marked: %v", e)
	}
	if e := bugReplyNotApplied(store.ErrGraphOwnerWait); !errors.Is(e, ErrBugReplyNotApplied) || !errors.Is(e, store.ErrGraphOwnerWait) {
		t.Fatalf("wait refusal lost its cause: %v", e)
	}
	for _, kept := range []error{context.Canceled, store.ErrGraphReplyPredatesQuestion} {
		if e := bugReplyNotApplied(kept); errors.Is(e, ErrBugReplyNotApplied) {
			t.Fatalf("%v was turned into a refusal", kept)
		}
	}
}

// An approval followed by a note on the next line is still an approval, not
// guidance that reruns the analyst.
func TestGraphBugApprovalReadsFirstLine(t *testing.T) {
	f, _, p, inst := conversationFixture(t)
	activities := ActivityRegistry{}
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: p, Reporter: f.opts.ResearchReporter}))
	_, e := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: f.opts.MCP.Now()})
	mustProgress(t, e)
	completeConversationAnalyst(t, f, inst, "done")
	_, e = ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: f.opts.MCP.Now()})
	mustProgress(t, e)
	wait, e := f.db.GetGraphOwnerWait(t.Context(), inst.ID)
	mustProgress(t, e)
	delivery, e := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, e)
	reply := store.GraphOwnerReply{InstanceID: inst.ID, OwnerID: 7, AuthorID: 7, CommentID: delivery.CommentID + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, At: f.opts.MCP.Now().Add(time.Minute)}
	for _, bad := range []string{"/approve-bug 1", "/approve-bug analysis one", "/approve-bug analysis 1 now"} {
		reply.Body = bad
		if _, e = AdmitGraphBugOwnerReply(t.Context(), f.db, reply); e == nil {
			t.Fatalf("%q was admitted", bad)
		}
	}
	reply.Body = "/approve-bug analysis 1.\r\nLooks good, go ahead."
	_, e = AdmitGraphBugOwnerReply(t.Context(), f.db, reply)
	mustProgress(t, e)
	_, approval, e := f.db.CurrentGraphDiagnosis(t.Context(), inst.ID)
	mustProgress(t, e)
	if approval == nil {
		t.Fatal("approval with a note on the next line did not approve")
	}
	released, e := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: reply.At})
	mustProgress(t, e)
	if released.NextNode != "fixer" {
		t.Fatalf("approval did not release the fixer: %+v", released)
	}
}

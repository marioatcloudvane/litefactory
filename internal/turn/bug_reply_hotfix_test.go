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

func TestBugUntouchedRecoveryRefusesExecutionEvidence(t *testing.T) {
	for _, evidence := range []string{"attempt", "dispatch", "run", "transition", "comment", "owner wait", "advanced node", "due time", "finished time", "foreign author", "read failure"} {
		t.Run(evidence, func(t *testing.T) {
			f, runtime, req := bugBootstrapFixture(t, nil)
			ctx := t.Context()
			body := req
			body.Trigger.CommentID = 0
			original, err := runtime.graphAdmission(ctx, body)
			mustProgress(t, err)
			instance := original.Instance
			at := f.opts.MCP.Now()
			switch evidence {
			case "attempt", "comment", "owner wait":
				attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, StartedAt: at})
				mustProgress(t, err)
				if evidence != "attempt" {
					mustProgress(t, f.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
					kind := "final"
					if evidence == "owner wait" {
						kind = "question"
					}
					mustProgress(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "retained-evidence", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: instance.CurrentNode, Kind: kind, Candidate: []byte(`{"comment":"retained"}`)}))
				}
			case "dispatch":
				dispatch, err := f.db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
				mustProgress(t, err)
				mustProgress(t, f.db.FinishGraphDispatch(ctx, dispatch.ID, at))
				// Even a finished dispatch with no next due time prevents recovery.
				mustProgress(t, f.db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphRunning, time.Time{}, time.Time{}))
			case "run":
				segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 1, "earlier-work", store.AgentClaude)
				mustProgress(t, err)
				_, err = f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 12, LogPath: func(int) string { return "earlier.log" }})
				mustProgress(t, err)
			case "transition":
				_, err := f.db.RecordGraphTransition(ctx, store.GraphTransition{InstanceID: instance.ID, FromNode: "analyst", ToNode: instance.CurrentNode, OnOutcome: "done", At: at})
				mustProgress(t, err)
			case "advanced node":
				instance.CurrentNode = "analyst"
			case "due time":
				instance.NextDueAt = at
			case "finished time":
				instance.FinishedAt = at
			case "foreign author":
				req.Trigger.AuthorID++
			case "read failure":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if evidence != "foreign author" {
				untouched, err := runtime.untouchedGraphLaunch(ctx, instance)
				if untouched {
					t.Fatal("execution evidence admitted as untouched")
				}
				if evidence == "read failure" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("read error lost: %v", err)
					}
				} else {
					mustProgress(t, err)
				}
			}
			if evidence == "advanced node" || evidence == "due time" || evidence == "finished time" {
				return
			}
			_, err = runtime.graphAdmission(ctx, req)
			if err == nil {
				t.Fatal("fresh launch admitted over retained evidence")
			}
			if evidence == "read failure" && !errors.Is(err, context.Canceled) {
				t.Fatalf("admission lost read error: %v", err)
			}
			captured, err := f.db.HasWorkflowLaunch(t.Context(), f.session.ID, req.Trigger.CommentID)
			mustProgress(t, err)
			if captured {
				t.Fatal("refused owner input captured a new launch")
			}
		})
	}
}

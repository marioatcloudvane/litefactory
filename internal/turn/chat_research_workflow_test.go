package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestChatAnswerContinuesResearchThroughDispatch(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if calls == 1 {
			submitLiveResult(t, req, researchAskJSON)
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchAskJSON, FinishedAt: time.Now()}, nil
		}
		if !strings.Contains(req.Prompt, "Inspect the HTTP boundary") {
			t.Fatal("continuation lost the chat answer")
		}
		client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
		update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"},{"op":"set","target":{"field":"comment"},"value":"Investigation finished."},{"op":"remove","target":{"field":"reason"}},{"op":"remove","target":{"field":"questions"}}]}`)
		submitted := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update))
		if !submitted.Validation.Valid {
			t.Fatalf("continuation result: %+v", submitted)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchDoneJSON, FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "Inspect the HTTP boundary", At: time.Now()}
	claimed, err := f.db.ClaimChatResearchReply(t.Context(), reply)
	mustProgress(t, err)
	due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 10)
	mustProgress(t, err)
	if len(due) != 1 {
		t.Fatalf("continuation dispatches: %v", due)
	}
	_, reserved, err := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
	mustProgress(t, err)
	if !reserved {
		t.Fatal("continuation not reserved")
	}
	req.Trigger.CommentID = due[0].TriggerID()
	req.Trigger.Body = ""
	req.Command = ParseCommand("/research inspect boundaries")
	done, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	after, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if !done.Succeeded() || calls != 2 || forge.attemptCount() != 2 || after.ReplyID != claimed.ReplyID || after.ReplySource != "chat" || after.ContinuationAttemptID == 0 {
		t.Fatalf("chat continuation failed: calls=%d posts=%d outcome=%+v wait=%+v", calls, forge.attemptCount(), done, after)
	}
}

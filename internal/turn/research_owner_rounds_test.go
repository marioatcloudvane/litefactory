package turn

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// TestResearchOwnerRoundsDoNotUseUpNodeLimits holds a shaping conversation for
// more owner rounds than the shipped node limit (#984). Every reply resumes the
// instance with a fresh allowance, so each one reaches the shaping agent. The
// last part reproduces #984's record: an instance failed by owner rounds with
// its last reply still claimed. A new owner comment must continue the work.
func TestResearchOwnerRoundsDoNotUseUpNodeLimits(t *testing.T) {
	calls, revision := 0, 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if revision == 0 {
			submitLiveResult(t, req, researchDoneJSON)
		} else {
			client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
			update := []byte(fmt.Sprintf(`{"expected_revision":%d,"operations":[{"op":"set","target":{"field":"comment"},"value":"Investigation finished."}]}`, revision))
			if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
				t.Fatalf("stage update on call %d (%s) refused", calls, req.Graph.Node)
			}
		}
		revision++
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchDoneJSON, FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledResearchFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	req.Trigger.CommentID++
	req.Trigger.Body = "Continue the admitted graph"
	req.Command = ParseCommand(req.Trigger.Body)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)

	claim := func(body string) store.GraphOwnerWait {
		t.Helper()
		wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
		mustProgress(t, err)
		if wait.State != "waiting" {
			t.Fatalf("owner round has no open question: %+v", wait)
		}
		delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
		mustProgress(t, err)
		reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: max(req.Trigger.CommentID, delivery.CommentID) + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: body, At: time.Now()}
		claimed, err := f.db.ClaimGraphOwnerReply(t.Context(), reply)
		mustProgress(t, err)
		req.Trigger.CommentID, req.Trigger.Body = reply.CommentID, reply.Body
		req.Command = ParseCommand(reply.Body)
		return claimed
	}

	const limit, rounds = 8, 11
	for round := 1; round <= rounds; round++ {
		claim(fmt.Sprintf("Follow-up question %d", round))
		before := calls
		_, err = Execute(t.Context(), f.opts, req)
		if err != nil || calls != before+1 {
			t.Fatalf("owner round %d did not reach the shaping agent: calls=%d err=%v", round, calls-before, err)
		}
	}
	current, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	mustProgress(t, err)
	if current.Status != store.GraphRunning {
		t.Fatalf("owner rounds ended the conversation: %+v", current)
	}
	if shaped := countAttempts(t, f, instance.ID, "shape"); shaped <= limit {
		t.Fatalf("owner rounds stayed within the node limit (%d shape attempts); the test proves nothing", shaped)
	}

	// #984: the factory failed the instance at "shape" and left the reply claimed.
	claim("why do we need this?")
	mustProgress(t, f.db.SetGraphInstanceStatus(t.Context(), instance.ID, store.GraphFailed, time.Time{}, time.Now()))
	req.Trigger.CommentID++
	req.Trigger.Body = "Please continue from the retained findings"
	req.Command = ParseCommand(req.Trigger.Body)
	before := calls
	revision = 0 // the successor starts its own retained result
	_, err = Execute(t.Context(), f.opts, req)
	if err != nil || calls != before+1 {
		t.Fatalf("new owner comment did not continue the exhausted conversation: calls=%d err=%v", calls-before, err)
	}
	successor, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	if successor.ID == instance.ID || successor.Status != store.GraphRunning {
		t.Fatalf("no running successor: %+v", successor)
	}
}

func countAttempts(t *testing.T, f *fixture, instanceID int64, node string) int {
	t.Helper()
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instanceID)
	mustProgress(t, err)
	n := 0
	for _, a := range attempts {
		if a.Node == node {
			n++
		}
	}
	return n
}

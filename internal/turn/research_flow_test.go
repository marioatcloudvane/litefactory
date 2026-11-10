package turn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

const researchFlowAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Only the owner knows the audience","comment":"One choice is open.","questions":[{"id":"q1","audience":"business","text":"Who is the export for?","options":[{"id":"a","text":"Finance"},{"id":"b","text":"Support"}],"recommended_option":"a"}],"payload":{"research":{"request":"Add a CSV export"}}}}`

var researchSessionLine = regexp.MustCompile(`Factory supplied session artifact directory: (\S+)`)

func researchInstance(t *testing.T, f *fixture) store.GraphInstance {
	t.Helper()
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	return instance
}

// finishResearchWithBrief writes RESEARCH_BRIEF.md where the factory told the
// agent to, then revises the saved question into a finished result that
// declares the brief.
func finishResearchWithBrief(t *testing.T, req agent.Request) {
	t.Helper()
	match := researchSessionLine.FindStringSubmatch(req.Prompt)
	if match == nil {
		t.Fatal("the invocation names no session artifact directory")
	}
	brief := match[1] + "/" + ResearchBriefName
	path := filepath.Join(req.WorktreePath, filepath.FromSlash(brief))
	mustProgress(t, os.MkdirAll(filepath.Dir(path), 0o750))
	mustProgress(t, os.WriteFile(path, []byte("# Research brief\n\n### O-01 Finance can export invoices as CSV (owner said: \"a\")\n"), 0o600))
	update := fmt.Sprintf(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"},{"op":"set","target":{"field":"comment"},"value":"Scope is set: a CSV export for Finance."},{"op":"remove","target":{"field":"reason"}},{"op":"remove","target":{"field":"questions"}},{"op":"set","target":{"field":"artifacts"},"value":[{"id":"brief","kind":"brief","path":%q}]}]}`, brief)
	client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
	if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", []byte(update))); !got.Validation.Valid {
		t.Fatalf("finished result refused: %+v", got.Validation)
	}
}

// Research is one agent: it asks, the owner's reply resumes the same agent
// with its saved result, and its finished result is posted as the final
// report, which completes the instance. There is no approval step.
func TestResearchAskReplyDoneCompletes(t *testing.T) {
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		requests = append(requests, req)
		if len(requests) == 1 {
			submitLiveResult(t, req, researchFlowAskJSON)
		} else {
			finishResearchWithBrief(t, req)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "turn", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance := researchInstance(t, f)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || instance.Status != store.GraphRunning || instance.CurrentNode != "waiting" || forge.attemptCount() != 1 {
		t.Fatalf("the question did not post and wait: %+v %+v posts=%d", instance, wait, forge.attemptCount())
	}
	if !strings.Contains(spikeLastBody(forge), "Who is the export for?") {
		t.Fatalf("question was not posted: %q", spikeLastBody(forge))
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "a - Finance", At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	req.Trigger.CommentID, req.Trigger.Body = reply.CommentID, reply.Body
	req.Command = ParseCommand(reply.Body)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || len(requests) != 2 || forge.attemptCount() != 2 {
		t.Fatalf("the reply did not finish Research: %+v runs=%d posts=%d err=%v", out, len(requests), forge.attemptCount(), err)
	}
	if r := requests[1]; r.Graph == nil || r.Graph.Node != "research" || !strings.Contains(r.Prompt, "a - Finance") || !strings.Contains(r.Prompt, "Who is the export for?") {
		t.Fatalf("the reply did not resume the same agent with its saved result: %+v", r.Graph)
	}
	final := spikeLastBody(forge)
	if !strings.Contains(final, "Scope is set: a CSV export for Finance.") || !strings.Contains(final, ResearchBriefName) {
		t.Fatalf("the final report was not posted with its brief: %q", final)
	}
	if instance := researchInstance(t, f); instance.Status != store.GraphSucceeded || instance.CurrentNode != "complete" {
		t.Fatalf("instance %+v", instance)
	}
	intents, err := f.db.ListGraphComments(t.Context(), instance.ID, "", 100)
	mustProgress(t, err)
	// ListGraphComments orders by a hashed delivery key, so order by attempt.
	sort.Slice(intents, func(i, j int) bool { return intents[i].SourceAttemptID < intents[j].SourceAttemptID })
	if len(intents) != 2 || intents[0].Kind != "question" || intents[1].Kind != "final" {
		t.Fatalf("want a question then the final report, got %+v", intents)
	}
}

// A failed attempt posts the failure and stops.
func TestResearchAgentFailurePostsAndStops(t *testing.T) {
	runner := lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		return &agent.RunResult{Outcome: agent.OutcomeTimedOut, Reason: "timeout", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed || forge.attemptCount() != 1 || researchInstance(t, f).Status != store.GraphFailed {
		t.Fatalf("agent failure went unreported: %+v posts=%d err=%v", out.Run, forge.attemptCount(), err)
	}
	intents, e := f.db.ListGraphComments(t.Context(), researchInstance(t, f).ID, "", 100)
	mustProgress(t, e)
	if len(intents) != 1 || intents[0].Kind != "failure" {
		t.Fatalf("want exactly the failure comment, got %+v", intents)
	}
}

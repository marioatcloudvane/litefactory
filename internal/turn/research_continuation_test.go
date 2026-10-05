package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

const researchContinueJSON = `{"version":1,"response":"continue","result":{"outcome":"incomplete","comment":"Boundary findings retained.","reason":"Trace remaining callers.","payload":{"research":{"request":"Inspect boundaries","continue":true,"findings":[{"id":"f1","text":"SQL stays in the store."}]}}}}`

func TestResearchSilentContinuationAndExhaustion(t *testing.T) {
	for _, stage := range []string{"investigate", "shape"} {
		t.Run(stage, func(t *testing.T) {
			passes := 0
			calls := 0
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				raw := researchContinueJSON
				if stage == "shape" && req.Graph.Node == "investigate" {
					raw = researchDoneJSON
				} else {
					passes++
					if passes > 1 && (!strings.Contains(req.Prompt, "Saved cumulative same-stage Research findings") || !strings.Contains(req.Prompt, "SQL stays in the store.")) {
						t.Fatal("lost cumulative findings")
					}
				}
				if calls == 1 {
					submitLiveResult(t, req, raw)
				} else {
					submitContinuationUpdate(t, req)
				}
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: raw, FinishedAt: time.Now()}, nil
			})
			f, forge, req := assembledResearchFixture(t, runner)
			_, err := Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			basePosts := 0
			if stage == "shape" {
				basePosts = 1
			}
			for i := 0; i < 9; i++ {
				instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
				mustProgress(t, err)
				if instance.Status == store.GraphFailed {
					break
				}
				due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 10)
				mustProgress(t, err)
				if len(due) != 1 || due[0].Node != stage {
					t.Fatalf("next stage: %+v", due)
				}
				if forge.attemptCount() != basePosts {
					t.Fatal("continuation posted progress")
				}
				if _, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("continuation opened wait: %v", err)
				}
				if i == 0 { // Reopen durable state before dispatching the next pass.
					mustProgress(t, f.db.Close())
					reopened, err := store.Open(t.Context(), f.paths.DBPath())
					mustProgress(t, err)
					mustProgress(t, reopened.Migrate(t.Context()))
					*f.db = *reopened
				}
				_, reserved, err := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
				mustProgress(t, err)
				if !reserved {
					t.Fatal("dispatch not reserved")
				}
				req.Trigger.CommentID = due[0].TriggerID()
				req.Trigger.Body = ""
				req.Command = ParseCommand("/research inspect boundaries")
				_, err = Execute(t.Context(), f.opts, req)

				if err != nil && !strings.Contains(err.Error(), "exhausted") {
					t.Fatal(err)
				}
			}
			instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
			mustProgress(t, err)
			if instance.Status != store.GraphFailed || passes != 8 || forge.attemptCount() != basePosts+1 {
				t.Fatalf("bounded loop passes=%d posts=%d instance=%+v", passes, forge.attemptCount(), instance)
			}
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
			mustProgress(t, err)
			count := 0
			for _, a := range attempts {
				if a.Node == stage {
					count++
					if a.Status != store.GraphSucceeded {
						t.Fatal("exhaustion rewrote successful partial attempt")
					}
				}
			}
			if count != 8 {
				t.Fatalf("allocated %d attempts", count)
			}
		})
	}
}

func TestResearchContinuationStopsAfterFailedProcess(t *testing.T) {
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		submitLiveResult(t, req, researchContinueJSON)
		return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "provider failed after partial submission", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	if err == nil {
		t.Fatal("failed process continued")
	}
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 10)
	mustProgress(t, err)
	if instance.Status != store.GraphFailed || len(due) != 0 || forge.attemptCount() != 1 {
		t.Fatalf("failed process scheduled work: instance=%+v due=%+v posts=%d", instance, due, forge.attemptCount())
	}
	source, err := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "investigate")
	mustProgress(t, err)
	outputs, err := f.db.ListGraphOutputs(t.Context(), source.ID)
	mustProgress(t, err)
	if len(outputs) != 2 || outputs[0].Kind != store.GraphOutputJSON || outputs[1].Kind != store.GraphOutputDiagnostic {
		t.Fatalf("retained outputs=%+v", outputs)
	}
	node, d := agentresult.DecodeResearchNode(outputs[0].Payload, ResearchWorkflowPolicy().baseContext(true))
	if !d.Valid {
		t.Fatalf("invalid retained findings: %+v", d)
	}
	if node.Response != "continue" || len(node.Result.Payload.Research.Findings) != 1 || source.Status != store.GraphFailed {
		t.Fatal("failed process lost partial findings or was relabeled")
	}
}

func submitContinuationUpdate(t *testing.T, req agent.Request) {
	t.Helper()
	client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
	var probe agentresult.UpdateDiagnostics
	mustProgress(t, json.Unmarshal(mcpToolCall(t, client, "communication_preflight", []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Boundary findings retained."}]}`)).Result, &probe))
	update := fmt.Sprintf(`{"expected_revision":%d,"operations":[{"op":"set","target":{"field":"questions"},"value":[]},{"op":"set","target":{"field":"outcome"},"value":"incomplete"},{"op":"set","target":{"field":"reason"},"value":"Trace remaining callers."},{"op":"set","target":{"field":"comment"},"value":"Boundary findings retained."},{"op":"set","target":{"field":"payload.research.continue"},"value":true},{"op":"set","target":{"field":"payload.research.findings"},"value":[{"id":"f1","text":"SQL stays in the store."}]}]}`, probe.CurrentRevision)
	got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", []byte(update)))
	if !got.Validation.Valid {
		b, _ := json.Marshal(got.Validation)
		t.Fatalf("update refused %s", b)
	}
}

func TestResearchContinuationAfterOwnerAnswer(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		raw := researchContinueJSON
		if calls == 1 {
			raw = researchAskJSON
			submitLiveResult(t, req, raw)
		} else {
			if !strings.Contains(req.Prompt, "Inspect HTTP") {
				t.Fatal("lost owner guidance")
			}
			if calls == 3 && !strings.Contains(req.Prompt, "Previously consumed owner guidance") {
				t.Fatal("reused owner reply as fresh authority")
			}
			submitContinuationUpdate(t, req)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: raw, FinishedAt: time.Now()}, nil
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
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "Inspect HTTP", At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	req.Trigger.CommentID = reply.CommentID
	req.Trigger.Body = reply.Body
	req.Command = ParseCommand(reply.Body)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 10)
	mustProgress(t, err)
	if len(due) != 1 {
		t.Fatalf("owed work=%+v", due)
	}
	_, reserved, err := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
	mustProgress(t, err)
	if !reserved {
		t.Fatal("dispatch not reserved")
	}
	req.Trigger.CommentID = due[0].TriggerID()
	req.Command = ParseCommand("/research inspect boundaries")
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	after, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if calls != 3 || forge.attemptCount() != 1 || after.ReplyID != reply.CommentID || after.ContinuationAttemptID == 0 || after.State != "allocated" {
		t.Fatalf("owner continuation changed wait: calls=%d posts=%d wait=%+v", calls, forge.attemptCount(), after)
	}
}

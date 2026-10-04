package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func approvedGraphRepairFixture(t *testing.T) (*fixture, *graphexec.Profile, store.GraphInstance) {
	t.Helper()
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
	reply := store.GraphOwnerReply{InstanceID: inst.ID, OwnerID: 7, AuthorID: 7, CommentID: delivery.CommentID + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: "/approve-bug analysis 1", At: f.opts.MCP.Now().Add(time.Minute)}
	_, e = AdmitGraphBugOwnerReply(t.Context(), f.db, reply)
	mustProgress(t, e)
	_, e = ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: reply.At})
	mustProgress(t, e)
	return f, p, inst
}

func TestGraphFixerAcceptedSuccessRequiresSuccessfulProcess(t *testing.T) {
	for _, variant := range []string{"success", "empty terminal", "prose terminal", "malformed terminal", "conflicting terminal", "failed process", "source changed after submit"} {
		t.Run(variant, func(t *testing.T) {
			f, _, inst := approvedGraphRepairFixture(t)
			at := f.opts.MCP.Now()
			segment, e := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 2, "fixer", store.AgentClaude)
			mustProgress(t, e)
			run, e := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 120, StartedAt: at, LogPath: func(int) string { return "fixer.log" }})
			mustProgress(t, e)
			attempt, e := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: inst.ID, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: at})
			mustProgress(t, e)
			req := ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: inst.ID, NodeAttemptID: attempt.ID, RunID: run.ID, Node: "fixer", Attrs: map[string]string{"lf_response": "result"}}
			logRoot := t.TempDir()
			logRoot, e = filepath.EvalSymlinks(logRoot)
			mustProgress(t, e)
			mustProgress(t, os.WriteFile(filepath.Join(logRoot, "check.log"), []byte("actual controlled checks passed\n"), 0600))
			logHash, e := git.ObserveEvidenceLog(t.Context(), logRoot, "check.log")
			mustProgress(t, e)
			commands := []bugrepair.ValidationRequirement{{ID: "repo-check", Command: "make check", Directory: "."}}
			manifest := []bugrepair.ValidationInput{{Name: "controlled-toolchain", SHA256: strings.Repeat("d", 64)}}
			inputHash, e := bugrepair.ValidationInputsDigest(commands, manifest)
			mustProgress(t, e)
			source := strings.Repeat("a", 64)
			inputs := func() GraphBugRepairInputs {
				return GraphBugRepairInputs{Source: git.SourceIdentity{Head: strings.Repeat("b", 40), Digest: source}, TargetMain: strings.Repeat("c", 40), InputsDigest: inputHash, Commands: commands, InputManifest: manifest}
			}
			var service *AgentResultService
			var scope agentresult.Context
			var retained string
			calls := 0
			runner := newStubRunner(func(ctx context.Context, request agent.Request) (*agent.RunResult, error) {
				calls++
				if !strings.Contains(request.Prompt, retained) {
					t.Fatal("approved guidance omitted")
				}
				if !strings.Contains(retained, "\"Stops\":[{") || !strings.Contains(retained, "\"reason\":") {
					t.Fatal("retained continuation explanations omitted")
				}
				source = strings.Repeat("e", 64)
				zero := 0
				repair := &bugrepair.Evidence{Reservation: scope.Bug.Reservation, AnalysisDigest: scope.Bug.ApprovedDigest, SourceCommit: scope.Bug.SourceCommit, SourceTree: scope.Bug.SourceTree, Summary: "Guard empty input", Coverage: []bugrepair.Coverage{{Criterion: "C1", Changes: []string{"search.go"}, Tests: []string{"search_test.go"}}}, Validation: &bugrepair.Validation{Generation: fmt.Sprintf("attempt:%d:final", attempt.ID), SourceDigest: source, TargetMain: inputs().TargetMain, InputsDigest: inputHash, Commands: []bugrepair.CommandOutcome{{ID: "repo-check", Command: "make check", Directory: ".", ExitCode: &zero, Outcome: "passed", Log: bugrepair.LogReference{Path: "check.log", SHA256: logHash}}}, Conditions: []bugrepair.ConditionOutcome{{ID: "C1", Outcome: "passed", Log: bugrepair.LogReference{Path: "check.log", SHA256: logHash}}}}}
				data, e := json.Marshal(agentresult.CreateInput{Result: agentresult.Result{Outcome: "completed", Comment: "Approved bug fixed and checked", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "succeeded", Invocation: scope.Bug.Invocation, BaseRevision: scope.Bug.BaseRevision, Repair: repair}}}})
				mustProgress(t, e)
				d, e := service.Preflight(ctx, data)
				mustProgress(t, e)
				if !d.Valid {
					t.Fatalf("preflight=%+v", d)
				}
				submitted, e := service.Submit(ctx, "final-fixer", data)
				mustProgress(t, e)
				if !submitted.Validation.Valid {
					t.Fatalf("submit=%+v", submitted.Validation)
				}
				_, e = service.Receipt(ctx, run.ID, "final-fixer")
				mustProgress(t, e)
				if variant == "failed process" {
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "controlled crash after accepted input"}, nil
				}
				if variant == "source changed after submit" {
					// Later edits or a moved main no longer discard a validated repair.
					source = strings.Repeat("f", 64)
				}
				terminal := `{"request_id":"receipt-only"}`
				switch variant {
				case "empty terminal":
					terminal = ""
				case "prose terminal":
					terminal = "Checks finished; result submitted."
				case "malformed terminal":
					terminal = `{"version":`
				case "conflicting terminal":
					terminal = `{"version":1,"response":"blocked","result":{"reason":"not the accepted result"}}`
				}
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: terminal}, nil
			})
			f.opts.Runners = runnerSet(runner)
			sourcePreparations := 0
			handler := GraphBugFixerActivity{PrepareSource: func(context.Context, Options, ActivityRequest) error { sourcePreparations++; return nil }, Observe: func(context.Context, Options, ActivityRequest) (GraphBugRepairInputs, error) { return inputs(), nil }, LogRoot: func(ActivityRequest) string { return logRoot }, Prepare: func(ctx context.Context, _ Options, _ ActivityRequest, c agentresult.Context, r string) (ResearchInvocation, error) {
				scope = c
				retained = r
				var e error
				service, e = BindAgentResultService(ctx, AgentResultServiceOptions{DB: f.db, Attempt: store.ResultAttempt{Task: f.session.TaskRef, Workflow: "bug", WorkContext: fmt.Sprintf("graph:%d:fix:%d", inst.ID, attempt.ID), Attempt: "fixer-attempt", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, ReadTargets: agentresult.EditTargets("bug"), EditTargets: agentresult.EditTargets("bug"), Resolve: func(context.Context) (agentresult.Context, error) { return c, nil }})
				if e != nil {
					return ResearchInvocation{}, e
				}
				spec, e := agent.NewMCPLaunchSpec("/factory/litefactory", filepath.Join(logRoot, "descriptor.json"), "fixer-attempt", "communication_preflight", "communication_submit", "communication_receipt")
				if e != nil {
					return ResearchInvocation{}, e
				}
				ref, e := naming.ParseRef(f.session.TaskRef)
				if e != nil {
					return ResearchInvocation{}, e
				}
				request := agent.Request{Workflow: agent.WorkflowBug, Invocation: agent.InvocationResearchNode, Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: logRoot, Prompt: "Repair approved context\n" + r, Permissions: agent.Permissions{Profile: "bug-fixer"}, MCPLaunch: spec}
				cold := request
				cold.Prompt = "Cold approved context\n" + r
				return ResearchInvocation{Request: request, ColdRequest: &cold, Kind: agent.Kind(store.AgentClaude), Results: service}, nil
			}}
			out, e := handler.Execute(t.Context(), f.opts, req)
			if variant == "failed process" {
				if e == nil {
					t.Fatal("failed execution lost its cause")
				}
			} else {
				mustProgress(t, e)
			}
			if calls != 1 || sourcePreparations != 1 {
				t.Fatalf("agent calls=%d", calls)
			}
			if variant != "failed process" {
				if out.Outcome != "succeeded" {
					t.Fatalf("out=%+v", out)
				}
				_, e = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded})
				mustProgress(t, e)
				e = f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "succeeded", FinishedAt: at, InstanceStatus: store.GraphRunning, NextDueAt: at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: out.Output}}, Transition: &store.GraphTransition{InstanceID: inst.ID, FromNode: "fixer", ToNode: "finalization", OnOutcome: "succeeded", At: at}})
				mustProgress(t, e)
				eligible, e := CurrentGraphBugRepairEligibility(t.Context(), f.db, inst.ID)
				mustProgress(t, e)
				if (variant != "source changed after submit" && eligible.Repair.Validation.SourceDigest != source) || eligible.ResultID < 1 {
					t.Fatalf("eligibility=%+v", eligible)
				}
			} else {
				if out.Outcome == "succeeded" {
					t.Fatal("failed execution advanced")
				}
				if _, e = CurrentGraphBugRepairEligibility(t.Context(), f.db, inst.ID); e == nil {
					t.Fatal("failed execution created delivery authority")
				}
			}
		})
	}
}

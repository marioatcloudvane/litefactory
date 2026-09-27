package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// A factory-owned test policy uses Plan's existing result contract with its
// own response vocabulary. No production Plan graph or role handler is added.
// Its Respond hook maps the retained result to that vocabulary (#410): the
// response never comes from what the agent printed.
func workflowReusePolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Mode:    agent.ModePlan,
		Control: graphexec.ControlPolicy{Mode: agent.ModePlan, Responses: []string{"question", "ready"}, Defaults: []string{"question", "ready"}, Ask: "question", Approved: "accepted"},
		Respond: func(r agentresult.Result) (string, error) {
			if r.Outcome != "incomplete" {
				return "", fmt.Errorf("private Plan stage result is %q, not incomplete", r.Outcome)
			}
			if len(r.Questions) > 0 {
				return "question", nil
			}
			return "ready", nil
		},
		Decode: func(raw []byte, scope agentresult.Context) (agentresult.ResearchNode, agentresult.Diagnostics) {
			var node agentresult.ResearchNode
			d := agentresult.Decode(raw, &node)
			if !d.Valid || !d.Complete {
				return node, d
			}
			if node.Version != 1 || (node.Response != "question" && node.Response != "ready") || node.Result.Outcome != "incomplete" || (node.Response == "question") != (len(node.Result.Questions) > 0) {
				return node, agentresult.Diagnostics{}
			}
			return node, agentresult.ValidateResult(node.Result, scope)
		},
		Guidance: "Use the Plan application contract; private envelope responses are question and ready.",
		Contract: func(source store.GraphNodeAttempt, kind string, r agentresult.Result, scope agentresult.Context) error {
			d := agentresult.ValidateResult(r, scope)
			if !d.Valid || !d.Complete {
				return resultDiagnosticError(d)
			}
			if source.Status != store.GraphSucceeded || r.Outcome != "incomplete" || (kind != "question" && kind != "final") || (kind == "question") != (len(r.Questions) > 0) {
				return errors.New("invalid private Plan stage publication")
			}
			return nil
		},
		Render:   func(r agentresult.Result, _ string, _ naming.Ref, _ string) string { return r.Comment },
		NextStep: func(*graphexec.Profile, string, string) string { return "" },
	}
}

func workflowReuseStage(name, next string) string {
	return fmt.Sprintf(`
 %[1]s_loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=question,lf_default=question,lf_limit=5,lf_source=%[1]s];
 %[1]s [lf_action=lf_agent,lf_outcomes=success,lf_agent_markdown=%[1]s,lf_limit=5];
 %[1]s_if [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=question,lf_limit=5,lf_source=%[1]s];
 %[1]s_ask [lf_action=lf_ask,lf_outcomes=published,lf_kind=question,lf_limit=5,lf_source=%[1]s];
 %[1]s_report [lf_action=lf_comment,lf_outcomes=published,lf_kind=final,lf_limit=5,lf_source=%[1]s];
 %[1]s_loop -> %[1]s [on=true]; %[1]s_loop -> %[1]s_report [on=false];
 %[1]s -> %[1]s_if [on=success]; %[1]s_if -> %[1]s_ask [on=true]; %[1]s_if -> %[1]s_report [on=false];
 %[1]s_ask -> waiting [on=published]; %[1]s_report -> %[2]s [on=published];
 `, name, next)
}

func TestWorkflowReuseSecondModeFileRoleWaitRestart(t *testing.T) {
	policy := workflowReusePolicy()
	specs, err := WorkflowSpecs(policy)
	mustProgress(t, err)
	role := func(name string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("---\nname: " + name + "\ndescription: Bounded offline probe.\ntier: balanced\ntask: Inspect the fixture.\ntools: [Read]\n---\nFile-defined role " + name + " uses the existing Plan result contract.\n")}
	}
	source := fstest.MapFS{"probe.md": role("probe")}
	graph := func(stages string) []byte {
		return []byte(`digraph {graph [team=test,mode=plan,profile_version=1,start=probe_loop]; waiting [lf_terminal=true]; complete [lf_terminal=true];` + stages + `}`)
	}
	source["workflow.dot"] = &fstest.MapFile{Data: graph(workflowReuseStage("probe", "complete"))}
	original, err := teamsource.CaptureWorkflow(t.Context(), source, "workflow.dot", "test", agent.ModePlan, specs)
	mustProgress(t, err)
	// Adding a role and stage changes only package files; the policy and registry stay identical.
	source["extra.md"] = role("extra")
	source["workflow.dot"] = &fstest.MapFile{Data: graph(workflowReuseStage("probe", "extra_loop") + workflowReuseStage("extra", "complete"))}
	definition, err := teamsource.CaptureWorkflow(t.Context(), source, "workflow.dot", "test", agent.ModePlan, specs)
	mustProgress(t, err)
	if len(original.Nodes) != 1 || len(definition.Nodes) != 2 || original.Digest == definition.Digest {
		t.Fatal("file-only role addition was not captured")
	}
	calls := 0
	var invocations []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		invocations = append(invocations, req)
		expectedRole := "probe"
		if calls == 3 {
			expectedRole = "extra"
		}
		if req.Mode != "plan" || req.Graph == nil || req.Usage == nil || req.Usage.AttemptID == "" || req.Delivery == nil || req.Delivery.MainRole != expectedRole {
			t.Fatalf("mode/role delivery: %+v", req)
		}
		roleBytes, e := os.ReadFile(filepath.Join(req.Delivery.Root, "plugin/agents/"+expectedRole+".md"))
		mustProgress(t, e)
		if !strings.Contains(string(roleBytes), "File-defined role "+expectedRole) || strings.Contains(string(roleBytes), "Bash") {
			t.Fatalf("captured role or permission intersection lost: %s", roleBytes)
		}
		for _, tool := range req.MCPLaunch.PermittedTools() {
			if tool == "issues_comment" {
				t.Fatal("private agent received publication authority")
			}
		}
		response, outcome, comment, reason := "ready", "incomplete", "Probe finished.", "Private stage ready; whole Plan remains unfinished"
		var questions []agentresult.Question
		if calls == 1 {
			response, outcome, comment, reason = "question", "incomplete", "Probe needs an owner choice.", "Choose the boundary"
			questions = []agentresult.Question{{ID: "Q-1", Audience: "business", Text: "Which boundary?"}}
		}
		if calls == 2 && (!strings.Contains(req.Prompt, "Use HTTP") || !strings.Contains(req.Prompt, "Probe needs an owner choice.")) {
			t.Fatal("continuation lost retained candidate or reply")
		}
		if calls == 2 {
			client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
			updated := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"incomplete"},{"op":"set","target":{"field":"comment"},"value":"Probe finished."},{"op":"set","target":{"field":"reason"},"value":"Private stage ready; whole Plan remains unfinished"},{"op":"remove","target":{"field":"questions"}}]}`)))
			if !updated.Validation.Valid {
				t.Fatalf("owner continuation candidate correction: %+v", updated)
			}
		}
		if calls == 3 {
			client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
			updated := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", []byte(`{"expected_revision":2,"operations":[{"op":"set","target":{"field":"comment"},"value":"Probe finished."}]}`)))
			if !updated.Validation.Valid {
				t.Fatalf("new stage retained baseline update: %+v", updated)
			}
		}
		raw, e := json.Marshal(agentresult.ResearchNode{Version: 1, Response: response, Result: agentresult.Result{Outcome: outcome, Comment: comment, Reason: reason, Questions: questions, Payload: agentresult.Payload{Plan: &agentresult.Plan{Summary: "Inspect fixture"}}}})
		mustProgress(t, e)
		if calls == 1 {
			submitLiveResult(t, req, string(raw))
		}
		// Nothing routable is printed: question and ready come from the retained
		// result through the policy's Respond hook alone (#410).
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "Probe stage submitted; " + response + " was meant.", FinishedAt: time.Now()}, nil
	})
	f, forge, _ := assembledResearchFixture(t, runner)
	req := f.request()
	req.Mode = agent.ModePlan
	req.Trigger.Body = "/plan inspect fixture"
	req.Command = ParseCommand(req.Trigger.Body)
	req.Tools = []string{"Read"}
	_, err = f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "graph")
	mustProgress(t, err)
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "test", Mode: "plan", ProfileVersion: 1, Path: "workflow.dot", Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	mustProgress(t, err)
	topology, err := graphexec.SemanticDigest(definition.Profile)
	mustProgress(t, err)
	files, err := json.Marshal(definition.Files)
	mustProgress(t, err)
	admission, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "plan", SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Reply: req.Trigger.Body})
	mustProgress(t, err)
	run := func() Result {
		session, e := f.db.GetSession(t.Context(), f.session.ID)
		mustProgress(t, e)
		out, e := ExecuteWorkflow(t.Context(), f.opts, req, Result{Session: session, SessionID: session.ID, Ref: f.ref()}, definition, admission, policy)
		mustProgress(t, e)
		if !out.Succeeded() {
			t.Fatalf("workflow failed: %+v", out)
		}
		return out
	}
	first := run()
	wait, err := f.db.GetGraphOwnerWait(t.Context(), admission.Instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || calls != 1 || forge.attemptCount() != 1 {
		t.Fatalf("durable question missing: %+v calls=%d posts=%d", wait, calls, forge.attemptCount())
	}
	mustProgress(t, f.db.Close())
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	mustProgress(t, reopened.Migrate(t.Context()))
	*f.db = *reopened
	n, err := f.db.ReconcileWorkflowGraphs(t.Context(), "plan", time.Now())
	mustProgress(t, err)
	if n != 0 {
		t.Fatal("restart treated settled wait as interrupted work")
	}
	mustProgress(t, RecoverWorkflowPublications(t.Context(), f.opts, f.session.ID, policy))
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: admission.Instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "plan", DeliveryKey: wait.DeliveryKey, Body: "Use HTTP", At: time.Now()}
	wrong := reply
	wrong.Mode = "research"
	if _, err = f.db.ClaimGraphOwnerReply(t.Context(), wrong); err == nil {
		t.Fatal("cross-mode reply accepted")
	}
	wrong = reply
	wrong.AuthorID = 8
	if _, err = f.db.ClaimGraphOwnerReply(t.Context(), wrong); err == nil {
		t.Fatal("non-owner reply accepted")
	}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	if _, err = f.db.ClaimGraphOwnerReply(t.Context(), reply); err == nil {
		t.Fatal("duplicate owner reply accepted")
	}
	req.Trigger.CommentID = reply.CommentID
	req.Trigger.Body = reply.Body
	req.Command = ParseCommand(reply.Body)
	second := run()
	instance, err := f.db.GetGraphInstance(t.Context(), admission.Instance.ID)
	mustProgress(t, err)
	if instance.CurrentNode != "extra" || instance.Status != store.GraphRunning || instance.NextDueAt.IsZero() || calls != 2 || forge.attemptCount() != 2 {
		t.Fatalf("file-added stage was not parked durably: %+v", instance)
	}
	req.Trigger.CommentID++
	third := run()
	instance, err = f.db.GetGraphInstance(t.Context(), admission.Instance.ID)
	mustProgress(t, err)
	if instance.Status != store.GraphSucceeded || calls != 3 || forge.attemptCount() != 3 {
		t.Fatalf("added role did not finish: %+v calls=%d posts=%d", instance, calls, forge.attemptCount())
	}
	if invocations[0].Graph.AttemptID == invocations[1].Graph.AttemptID || first.Run.ID == second.Run.ID || second.Run.ID == third.Run.ID {
		t.Fatal("continuation reused attempt/run identity")
	}
	for i, request := range invocations {
		meter, e := f.db.GetMeterAttempt(t.Context(), request.Usage.AttemptID)
		mustProgress(t, e)
		if meter.RunID != request.Graph.RunID || meter.Outcome != "success" {
			t.Fatalf("meter identity/outcome lost: %+v", meter)
		}
		if i > 0 && request.Usage.AttemptID == invocations[i-1].Usage.AttemptID {
			t.Fatal("distinct runs reused meter identity")
		}
	}
	for _, out := range []Result{first, second, third} {
		mustProgress(t, filepath.WalkDir(out.WorktreePath, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Name() == "RESEARCH_BRIEF.md" || d.Name() == "RESEARCH_TECHNICAL.md" {
				t.Fatalf("second mode generated Research document %s", path)
			}
			return nil
		}))
	}
	mustProgress(t, RecoverWorkflowPublications(t.Context(), f.opts, f.session.ID, policy))
	if forge.attemptCount() != 3 || calls != 3 {
		t.Fatal("recovery repeated acknowledged effects or provider")
	}
}

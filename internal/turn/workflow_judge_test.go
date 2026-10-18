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
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// The graph driver, SQLite storage, isolated judge result broker and delivery
// journal are real. Only the inference provider and remote forge are controlled.
func TestWorkflowJudgePrivateThenExplicitPublication(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan} {
		t.Run(string(mode), func(t *testing.T) {
			f, forge, req, source, runner := workflowJudgeFixture(t, mode)
			step, err := ExecuteNodeAction(t.Context(), f.opts, req)
			mustProgress(t, err)
			if step.Activity.Outcome != "success" || step.NextNode != "publish" || len(runner.calls) != 1 || forge.attemptCount() != 0 {
				t.Fatalf("judge did not privately retain advice: %+v calls=%d posts=%d", step, len(runner.calls), forge.attemptCount())
			}
			pinned, err := f.db.FindWorkflowJudgeRequest(t.Context(), req.InstanceID, step.AttemptID)
			mustProgress(t, err)
			if pinned.SourceNodeAttemptID != source.ID || pinned.SourceNode != "source" || len(pinned.Report) == 0 {
				t.Fatalf("report did not pin explicitly chosen source: %+v", pinned)
			}
			if pinned.Source.AcceptedResultID == 0 || pinned.Source.AcceptedRevision != 1 {
				t.Fatalf("accepted source result identity and revision were not pinned: %+v", pinned.Source)
			}
			if mode == agent.ModePlan && len(pinned.Source.PlanContext) == 0 {
				t.Fatalf("native Plan result and draft revision were not pinned: %+v", pinned.Source)
			}
			outputs, err := f.db.ListGraphOutputs(t.Context(), step.AttemptID)
			mustProgress(t, err)
			for _, output := range outputs {
				if strings.Contains(string(output.Payload), "Captured source inspected") {
					t.Fatal("report body escaped into graph output")
				}
			}
			if _, err = f.db.GetGraphOwnerWait(t.Context(), req.InstanceID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("private judge opened owner wait: %v", err)
			}
			step, err = ExecuteNodeAction(t.Context(), f.opts, req)
			mustProgress(t, err)
			if step.Activity.Outcome != "published" || forge.attemptCount() != 1 || step.NextNode != "gate" {
				t.Fatalf("explicit publication failed: %+v posts=%d", step, forge.attemptCount())
			}
			body := forge.only(t).body
			if !strings.Contains(body, "Captured source inspected") || !strings.Contains(body, "node source, attempt 1; graph:") {
				t.Fatalf("published report lacks source or retained assessment: %s", body)
			}
			if _, err = f.db.GetGraphOwnerWait(t.Context(), req.InstanceID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("advisory publication opened owner wait: %v", err)
			}
			// The same existing review boundary still blocks execution until a qualified
			// owner reply. An advisory report grants no authority to settle this wait.
			mustProgress(t, f.db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: "required-owner-review", InstanceID: req.InstanceID, SourceAttemptID: source.ID, Node: "gate", Kind: "review", Candidate: []byte(`{"comment":"Review saved source"}`)}))
			blocked, err := ExecuteNodeAction(t.Context(), f.opts, req)
			mustProgress(t, err)
			if blocked.Status != "waiting_owner" || blocked.AttemptID != 0 || len(runner.calls) != 1 {
				t.Fatalf("advice bypassed owner gate: %+v", blocked)
			}
			if watermark(t, f) != 0 {
				t.Fatal("advice consumed owner input")
			}
		})
	}
}

func workflowJudgeFixture(t *testing.T, mode agent.Mode) (*fixture, *fakeForge, GraphNodeRequest, store.GraphNodeAttempt, *recoveryJudgeRunner) {
	t.Helper()
	f := newFixture(t, nil)
	forge := newFakeForge(t)
	service, judgeContext, _ := judgeRecoveryFixture(t)
	service.DB = f.db
	pkg, err := instructionpackage.LoadShipped(t.Context(), "")
	mustProgress(t, err)
	judgeContext.Instructions, _, err = pkg.Capture(t.Context(), instructionpackage.JudgeRole)
	mustProgress(t, err)
	judgeContext.Ref = f.ref()
	judgeContext.SessionID = f.session.ID
	runner := &recoveryJudgeRunner{}
	service.Providers[agent.Kind("codex")] = runner
	service.Providers[agent.Kind("claude")] = runner
	f.opts.Judge = service
	dot := fmt.Sprintf(`digraph {team=builtin;mode=%s;profile_version=1;start=source;
source [lf_action=lf_agent,lf_outcomes=success];
judge [lf_action=lf_judge,lf_source=source,lf_limit=1,lf_agent=other,lf_outcomes="success,failed"];
publish [lf_action=lf_comment,lf_kind=advisory,lf_source=judge,lf_outcomes=published];
gate [lf_action=lf_ask,lf_kind=review,lf_source=source,lf_outcomes=published];
end [lf_terminal=true];source -> judge [on=success];judge -> publish [on=success];judge -> publish [on=failed];publish -> gate [on=published];gate -> end [on=published];}`, mode)
	profile, err := graphexec.ParseProfile([]byte(dot))
	mustProgress(t, err)
	var specs graphexec.Registry
	mustProgress(t, specs.Register(graphexec.ActionSpec{Name: ResearchAgentAction, Mode: mode, Outcomes: []string{"success"}}))
	mustProgress(t, specs.Register(graphexec.JudgeSpec(mode)))
	commentSpec, askSpec := ResearchCommentSpec(), ResearchAskSpec()
	commentSpec.Mode, askSpec.Mode = mode, mode
	mustProgress(t, specs.Register(commentSpec))
	mustProgress(t, specs.Register(askSpec))
	mustProgress(t, graphexec.Validate(profile, specs))
	mustProgress(t, graphexec.ValidateJudgeGraph(profile))
	at := time.Now().UTC()
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: string(mode), ProfileVersion: 1, Path: string(mode) + ".dot", Digest: profile.Digest, CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, Status: store.GraphRunning, CurrentNode: "source", StartedAt: at})
	mustProgress(t, err)
	resultAttempt := agentResultRun(t, f.db, f.session, string(mode), "source-attempt", 8)
	source, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "source", RunID: resultAttempt.RunID, Limit: 1, StartedAt: at})
	mustProgress(t, err)
	binding, err := f.db.BindResultAttempt(t.Context(), resultAttempt)
	mustProgress(t, err)
	payload := `{"research":{"request":"saved","findings":[{"id":"f1","text":"Saved conclusion"}]}}`
	if mode == agent.ModePlan {
		payload = `{"plan":{"storyrequest":{"operation":"create","draft":{"key":"story-001","name":"Saved plan","user_story":{"want":"bounded work"}}}}}`
	}
	saved := []byte(`{"result":{"outcome":"completed","comment":"Saved source","payload":` + payload + `}}`)
	_, err = f.db.RecordResult(t.Context(), store.ResultRecord{Binding: binding, RequestID: "saved-source", ExpectedRevision: 0, Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: saved, Retired: []byte(`[]`), Accepted: true})
	mustProgress(t, err)
	graphOutput := saved
	if mode == agent.ModePlan {
		graphOutput, err = retainPlanResult(binding, 1, agentresult.ResearchNode{Version: 1, Response: "done"})
		mustProgress(t, err)
	}
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: graphOutput}}, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "source", ToNode: "judge", OnOutcome: "success", At: at}}))
	req := GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: at}
	mustProgress(t, req.Activities.Register(graphexec.JudgeAction, JudgeActivity{Profile: profile, Service: service, Context: func(context.Context, ActivityRequest) (JudgeRequest, error) { return judgeContext, nil }}))
	policy := ResearchWorkflowPolicy()
	policy.Mode = mode
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	if mode == agent.ModePlan {
		runtime := &planGraphRuntime{opts: f.opts, definition: &teamsource.WorkflowDefinition{Profile: profile}}
		mustProgress(t, req.Activities.Register(ResearchCommentAction, planGraphHandler(runtime.comment)))
	} else {
		comment := CommentActivity{Profile: profile, Reporter: f.opts.ResearchReporter, Policy: policy, Context: func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error) {
			t.Fatal("advisory used source policy hooks")
			return agentresult.Context{}, nil
		}}
		mustProgress(t, req.Activities.Register(ResearchCommentAction, comment))
	}
	return f, forge, req, source, runner
}

func TestWorkflowJudgeAdvisoryDeliveryRecoveryPinsBytes(t *testing.T) {
	f, forge, req, _, runner := workflowJudgeFixture(t, agent.ModeResearch)
	_, err := ExecuteNodeAction(t.Context(), f.opts, req)
	mustProgress(t, err)
	forge.afterPost = func() { mustProgress(t, f.db.Close()) }
	_, err = ExecuteNodeAction(t.Context(), f.opts, req)
	if err == nil || forge.attemptCount() != 1 {
		t.Fatalf("lost receipt was hidden: %v", err)
	}
	original := forge.only(t).body
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	mustProgress(t, reopened.Migrate(t.Context()))
	*f.db = *reopened
	forge.afterPost = nil
	comments, err := f.db.ListGraphComments(t.Context(), req.InstanceID, "", 10)
	mustProgress(t, err)
	if len(comments) != 1 || comments[0].Kind != "advisory" {
		t.Fatalf("publication intent missing: %+v", comments)
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), comments[0].Key)
	mustProgress(t, err)
	if !delivery.PossiblySent || delivery.Completed || delivery.CommentID != 0 {
		t.Fatalf("uncertain delivery did not remain pending: %+v", delivery)
	}
	reporter := newReporter(t, f, forge, stubRenderer{err: errors.New("retained bytes must not be rendered again")})
	_, err = reporter.RecoverWorkflowComment(t.Context(), comments[0].Key, agentresult.Context{Mode: "research"}, ResearchWorkflowPolicy())
	mustProgress(t, err)
	if forge.attemptCount() != 1 || forge.only(t).body != original || len(runner.calls) != 1 {
		t.Fatal("recovery changed bytes, reposted or reran judging")
	}
}

func TestWorkflowJudgeRetainedReportRecoversGraphBookkeeping(t *testing.T) {
	f, forge, req, _, runner := workflowJudgeFixture(t, agent.ModeResearch)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: req.InstanceID, Node: "judge", Limit: 1, StartedAt: req.At})
	mustProgress(t, err)
	handler, ok := req.Activities.Resolve(graphexec.JudgeAction)
	if !ok {
		t.Fatal("judge handler missing")
	}
	output, err := handler.Execute(t.Context(), f.opts, ActivityRequest{Team: req.Profile.Team, Mode: agent.ModeResearch, Workflow: agent.WorkflowResearch, Node: "judge", Attrs: map[string]string{}, InstanceID: req.InstanceID, NodeAttemptID: attempt.ID})
	mustProgress(t, err)
	if output.Outcome != "success" || len(runner.calls) != 1 {
		t.Fatalf("initial assessment: %+v", output)
	}
	// Simulate a crash after report retention and before graph completion. Recovery
	// must use that report even though the graph's positive limit is already spent.
	req.ResumeAttemptID = attempt.ID
	recovered, err := ExecuteNodeAction(t.Context(), f.opts, req)
	mustProgress(t, err)
	if recovered.AttemptID != attempt.ID || recovered.NextNode != "publish" || string(recovered.Activity.Output) != string(output.Output) || len(runner.calls) != 1 || forge.attemptCount() != 0 {
		t.Fatalf("recovery relaunched or lost pinned output: %+v", recovered)
	}
}

func TestWorkflowJudgeAbandonedReservationFailsWithoutInference(t *testing.T) {
	f, forge, req, _, runner := workflowJudgeFixture(t, agent.ModeResearch)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: req.InstanceID, Node: "judge", Limit: 1, StartedAt: req.At})
	mustProgress(t, err)
	req.ResumeAttemptID = attempt.ID
	recovered, err := ExecuteNodeAction(t.Context(), f.opts, req)
	mustProgress(t, err)
	if recovered.Activity.Outcome != "failed" || !strings.Contains(string(recovered.Activity.Output), "abandoned") || len(runner.calls) != 0 || forge.attemptCount() != 0 {
		t.Fatalf("abandoned request blindly launched: %+v", recovered)
	}
	req.ResumeAttemptID = 0
	published, err := ExecuteNodeAction(t.Context(), f.opts, req)
	mustProgress(t, err)
	if published.Activity.Outcome != "published" || !strings.Contains(forge.only(t).body, "abandoned") || len(runner.calls) != 0 {
		t.Fatalf("failure explanation was not explicitly published: %+v", published)
	}
}

func TestWorkflowWithoutJudgePreservesExistingOwnerWait(t *testing.T) {
	f := newFixture(t, nil)
	profile, err := graphexec.ParseProfile([]byte(`digraph {team=builtin;mode=research;profile_version=1;start=source;
source [lf_action=lf_agent,lf_outcomes=success]; gate [lf_action=lf_ask,lf_kind=review,lf_source=source,lf_outcomes=published];end [lf_terminal=true];source -> gate [on=success];gate -> end [on=published];}`))
	mustProgress(t, err)
	at := time.Now().UTC()
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: profile.Digest, CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, Status: store.GraphRunning, CurrentNode: "source", StartedAt: at})
	mustProgress(t, err)
	result := agentResultRun(t, f.db, f.session, "research", "ordinary-source", 8)
	source, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "source", RunID: result.RunID, Limit: 1, StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "source", ToNode: "gate", OnOutcome: "success", At: at}}))
	mustProgress(t, f.db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: "ordinary-owner-review", InstanceID: instance.ID, SourceAttemptID: source.ID, Node: "gate", Kind: "review", Candidate: []byte(`{"comment":"Review saved source"}`)}))
	blocked, err := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: at})
	mustProgress(t, err)
	if blocked.Status != "waiting_owner" || blocked.AttemptID != 0 {
		t.Fatalf("ordinary workflow gate changed: %+v", blocked)
	}
}

type workflowDeadlineJudgeRunner struct {
	runner    *recoveryJudgeRunner
	remaining time.Duration
}

func (r *workflowDeadlineJudgeRunner) RunJudge(ctx context.Context, req agent.JudgeRequest) (agent.JudgeRunResult, error) {
	if deadline, ok := ctx.Deadline(); ok {
		r.remaining = time.Until(deadline)
	}
	return r.runner.RunJudge(ctx, req)
}

func TestWorkflowJudgeMapsOverridesThroughSharedSettings(t *testing.T) {
	f, _, req, _, runner := workflowJudgeFixture(t, agent.ModeResearch)
	deadlineRunner := &workflowDeadlineJudgeRunner{runner: runner}
	f.opts.Judge.Providers[agent.Kind("codex")] = deadlineRunner
	f.opts.Judge.Config.Judge.JudgeOptions = config.JudgeOptions{Agent: "claude", Model: "balanced", Thinking: "medium"}
	for i := range req.Profile.Nodes {
		if req.Profile.Nodes[i].Name != "judge" {
			continue
		}
		req.Profile.Nodes[i].Attrs["lf_agent"] = "codex"
		req.Profile.Nodes[i].Attrs["lf_model"] = "flagship"
		req.Profile.Nodes[i].Attrs["lf_thinking"] = "high"
		req.Profile.Nodes[i].Attrs["lf_focus"] = "Check evidence boundaries"
		req.Profile.Nodes[i].Attrs["lf_budget"] = "1"
	}
	out, err := ExecuteNodeAction(t.Context(), f.opts, req)
	mustProgress(t, err)
	if out.Activity.Outcome != "success" || len(runner.calls) != 1 {
		t.Fatalf("override execution: %+v", out)
	}
	saved, err := f.db.FindWorkflowJudgeRequest(t.Context(), req.InstanceID, out.AttemptID)
	mustProgress(t, err)
	var settings judgeConfiguration
	mustProgress(t, json.Unmarshal(saved.Configuration, &settings))
	if settings.Requested.Agent != "codex" || settings.Requested.Model != "flagship" || settings.Requested.Thinking != "high" || settings.Requested.Focus != "Check evidence boundaries" || settings.Effective.Provider != config.KindCodex || settings.Effective.Thinking != "high" || runner.calls[0].Invocation.Model != settings.Effective.Model {
		t.Fatalf("activity settings did not use shared resolver: %+v", settings)
	}
	if deadlineRunner.remaining <= 0 || deadlineRunner.remaining > time.Minute {
		t.Fatalf("node budget did not bound runner: %v", deadlineRunner.remaining)
	}
	if !strings.Contains(runner.calls[0].Invocation.Prompt, "Check evidence boundaries") {
		t.Fatal("focus missing from captured challenger prompt")
	}
}

func TestWorkflowJudgeStartupRecoveryBeforeGenericReconciliation(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(fmt.Sprintf("retained_%v", retained), func(t *testing.T) {
			f := newFixture(t, nil)
			service, _, _ := judgeRecoveryFixture(t)
			service.DB = f.db
			runner := &recoveryJudgeRunner{}
			service.Providers[agent.Kind("codex")] = runner
			service.Providers[agent.Kind("claude")] = runner
			f.opts.Judge = service
			specs, err := ResearchSpecs()
			mustProgress(t, err)
			definition, err := teamsource.CaptureResearch(t.Context(), specs)
			mustProgress(t, err)
			files := teamsource.RetainedFiles(definition.Files)
			original := `investigate -> brief       [lf_on="success"];`
			dot := string(files["research.dot"])
			if !strings.Contains(dot, original) {
				t.Fatal("shipped source edge missing")
			}
			dot = strings.Replace(dot, original, `investigate -> challenge [lf_on="success"];
challenge [lf_action="lf_judge",lf_source="investigate",lf_limit="1",lf_agent="other",lf_outcomes="success,failed"];
advice [lf_action="lf_comment",lf_kind="advisory",lf_source="challenge",lf_limit="1",lf_outcomes="published"];
challenge -> advice [lf_on="success"];
challenge -> advice [lf_on="failed"];
advice -> brief [lf_on="published"];`, 1)
			files["research.dot"] = []byte(dot)
			role, err := teamsource.JudgeInstructions()
			mustProgress(t, err)
			files["judge/challenger.md"] = role
			definition, err = teamsource.CaptureWorkflowIdentity(t.Context(), files, "research.dot", definition.Profile.Team, agent.WorkflowResearch, specs)
			mustProgress(t, err)
			topology, err := graphexec.SemanticDigest(definition.Profile)
			mustProgress(t, err)
			rawFiles, err := json.Marshal(definition.Files)
			mustProgress(t, err)
			at := time.Now().UTC()
			pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: definition.Profile.Team, Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: definition.Profile.Digest, CreatedAt: at})
			mustProgress(t, err)
			_, err = f.db.AdmitResearchPath(t.Context(), f.session.ID, "graph")
			mustProgress(t, err)
			admitted, err := f.db.AdmitResearchLaunch(t.Context(), store.ResearchLaunch{SessionID: f.session.ID, TriggerID: 10, DefinitionID: pin.ID, Start: "investigate", ContentDigest: definition.Digest, TopologyDigest: topology, Files: rawFiles})
			mustProgress(t, err)
			run := agentResultRun(t, f.db, f.session, "research", "startup-source", 10)
			source, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: "investigate", RunID: run.RunID, Limit: 8, StartedAt: at})
			mustProgress(t, err)
			raw := []byte(`{"result":{"outcome":"completed","payload":{"research":{"request":"saved","findings":[{"id":"f1","text":"Saved conclusion"}]}}}}`)
			binding, err := f.db.BindResultAttempt(t.Context(), run)
			mustProgress(t, err)
			_, err = f.db.RecordResult(t.Context(), store.ResultRecord{Binding: binding, RequestID: "startup-saved-source", ExpectedRevision: 0, Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: raw, Retired: []byte(`[]`), Accepted: true})
			mustProgress(t, err)
			mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: raw}}, Transition: &store.GraphTransition{InstanceID: source.InstanceID, FromNode: "investigate", ToNode: "challenge", OnOutcome: "success", At: at}}))
			attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: source.InstanceID, LaunchID: admitted.LaunchID, Node: "challenge", Limit: 1, StartedAt: at})
			mustProgress(t, err)
			if retained {
				handler := JudgeActivity{Profile: definition.Profile, Service: service}
				result, err := handler.Execute(t.Context(), f.opts, ActivityRequest{Mode: agent.ModeResearch, Workflow: agent.WorkflowResearch, Node: "challenge", InstanceID: source.InstanceID, NodeAttemptID: attempt.ID})
				mustProgress(t, err)
				if result.Outcome != "success" {
					t.Fatalf("initial report not retained: %+v", result)
				}
			}
			mustProgress(t, RecoverWorkflowJudges(t.Context(), f.opts, at.Add(time.Second)))
			_, err = f.db.ReconcileResearchGraphs(t.Context(), at.Add(2*time.Second))
			mustProgress(t, err)
			instance, err := f.db.GetGraphInstance(t.Context(), source.InstanceID)
			mustProgress(t, err)
			finished, err := f.db.GetGraphNodeAttempt(t.Context(), attempt.ID)
			mustProgress(t, err)
			expectedCalls, expectedOutcome := 0, "failed"
			if retained {
				expectedCalls, expectedOutcome = 1, "success"
			}
			if instance.Status != store.GraphRunning || instance.CurrentNode != "advice" || finished.Status != store.GraphSucceeded || finished.Outcome != expectedOutcome || len(runner.calls) != expectedCalls {
				t.Fatalf("startup discarded recoverable bookkeeping or relaunched: %+v %+v calls=%d", instance, finished, len(runner.calls))
			}
			comments, err := f.db.ListGraphComments(t.Context(), instance.ID, "", 10)
			mustProgress(t, err)
			if len(comments) != 0 {
				t.Fatal("startup recovery published advice")
			}
		})
	}
}

package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Every registered workflow policy, and the runtime form of each that adds
// handlers, travels the shared result route: it is admitted, names a decoder for
// the shared result and yields a registry.
func TestWorkflowRegistry_EveryPolicyRequiresSharedResultBindingAndDecoding(t *testing.T) {
	t.Parallel()
	policies := RegisteredWorkflowPolicies()
	if len(policies) < 2 {
		t.Fatalf("registry lost a shipped workflow: %d policies", len(policies))
	}
	// A running Plan adds its handlers to the registered policy and is walked too.
	policies = append(policies, (&planGraphRuntime{}).policy())
	modes := map[agent.Mode]bool{}
	for _, policy := range policies {
		name := string(policy.Mode)
		if err := policy.Admit(); err != nil {
			t.Errorf("%s: registered policy refused: %v", name, err)
		}
		if policy.Decode == nil {
			t.Errorf("%s: no decoder for the shared result", name)
		}
		if _, err := WorkflowSpecs(policy); err != nil {
			t.Errorf("%s: no registry: %v", name, err)
		}
		if err := RegisterAgent(&graphexec.Registry{}, &ActivityRegistry{}, AgentActivity{Policy: policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
			return ResearchInvocation{}, nil
		}}); err != nil {
			t.Errorf("%s: agent registration: %v", name, err)
		}
		if policy.Handlers != nil && (policy.Handlers.Prepare == nil || policy.Failure == nil || policy.Render == nil) {
			t.Errorf("%s: handlers prepare no result binding or report no failure", name)
		}
		if modes[policy.Mode] && policy.Handlers == nil {
			t.Errorf("%s: two registered policies share a mode", name)
		}
		modes[policy.Mode] = true
	}
}

// Admission refuses a policy that cannot judge a shared result, or whose own
// handlers bind no result service or report no failure, before any dispatch.
func TestWorkflowRegistry_AdmissionRefusesUnboundPolicy(t *testing.T) {
	t.Parallel()
	prepare := func(context.Context, *researchWorkflow, ActivityRequest) (ResearchInvocation, error) {
		return ResearchInvocation{}, nil
	}
	for _, tt := range []struct {
		name   string
		mutate func(*WorkflowPolicy)
	}{
		{"no decoder", func(p *WorkflowPolicy) { p.Decode = nil }},
		{"no mode", func(p *WorkflowPolicy) { p.Mode = "" }},
		{"foreign control mode", func(p *WorkflowPolicy) { p.Control.Mode = agent.ModePlan }},
		{"no guidance", func(p *WorkflowPolicy) { p.Guidance = "" }},
		{"handlers without preparation", func(p *WorkflowPolicy) { p.Handlers = &WorkflowHandlers{} }},
		{"handlers without failure builder", func(p *WorkflowPolicy) { p.Handlers, p.Failure = &WorkflowHandlers{Prepare: prepare}, nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := ResearchWorkflowPolicy()
			tt.mutate(&policy)
			if err := policy.Admit(); err == nil {
				t.Fatal("unbound policy admitted")
			}
			if _, err := WorkflowSpecs(policy); err == nil {
				t.Fatal("registry built for an unbound policy")
			}
			err := RegisterAgent(&graphexec.Registry{}, &ActivityRegistry{}, AgentActivity{Policy: policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				return ResearchInvocation{}, nil
			}})
			if err == nil {
				t.Fatal("agent registered under an unbound policy")
			}
		})
	}
}

// Decoder presence alone is not enough. A policy whose decoder accepts anything
// still cannot complete an attempt from the terminal text: with no retained
// accepted shared result the runtime fails the attempt with its cause, retains
// nothing, and no downstream effect follows.
func TestWorkflowRegistry_NominalDecoderCannotCompleteFromTerminalOutputAlone(t *testing.T) {
	var decoded, retained, authorized, artifacts int
	policy := workflowReusePolicy()
	policy.Decode = func(raw []byte, _ agentresult.Context) (agentresult.ResearchNode, agentresult.Diagnostics) {
		decoded++
		var node agentresult.ResearchNode
		if d := agentresult.Decode(raw, &node); !d.Valid {
			return node, d
		}
		return node, agentresult.Diagnostics{Valid: true, Complete: true}
	}
	policy.Retain = func(store.ResultBinding, int64, agentresult.ResearchNode) ([]byte, error) {
		retained++
		return []byte(`{}`), nil
	}
	policy.AuthorizeOutput = func(context.Context, *store.DB, store.GraphNodeAttempt, agentresult.ResearchNode) error {
		authorized++
		return nil
	}
	policy.Artifacts = func(string, string, agentresult.Result) error { artifacts++; return nil }
	specs, err := WorkflowSpecs(policy)
	mustProgress(t, err)
	role := &fstest.MapFile{Data: []byte("---\nname: probe\ndescription: Bounded offline probe.\ntier: balanced\ntask: Inspect the fixture.\ntools: [Read]\n---\nProbe role.\n")}
	source := fstest.MapFS{"probe.md": role, "workflow.dot": &fstest.MapFile{Data: []byte(`digraph {graph [team=test,mode=plan,profile_version=1,start=probe_loop]; waiting [lf_terminal=true]; complete [lf_terminal=true];` + workflowReuseStage("probe", "complete") + `}`)}}
	definition, err := teamsource.CaptureWorkflow(t.Context(), source, "workflow.dot", "test", agent.ModePlan, specs)
	mustProgress(t, err)
	terminal, err := json.Marshal(agentresult.ResearchNode{Version: 1, Response: "ready", Result: agentresult.Result{Outcome: "incomplete", Comment: "Probe finished.", Reason: "Private stage ready", Payload: agentresult.Payload{Plan: &agentresult.Plan{Summary: "Inspect fixture"}}}})
	mustProgress(t, err)
	calls := 0
	runner := lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		// Nothing is submitted: the terminal object is the only thing offered.
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(terminal), FinishedAt: time.Now()}, nil
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
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)

	out, err := ExecuteWorkflow(t.Context(), f.opts, req, Result{Session: session, SessionID: session.ID, Ref: f.ref()}, definition, admission, policy)
	if err == nil || out.Succeeded() || !strings.Contains(err.Error(), "no accepted shared result") {
		t.Fatalf("terminal-only completion was accepted or lost its cause: succeeded=%v err=%v", out.Succeeded(), err)
	}
	if calls != 1 || retained != 0 || authorized != 0 || artifacts != 0 {
		t.Fatalf("a downstream step ran on unaccepted output: calls=%d retain=%d authorize=%d artifacts=%d decode=%d", calls, retained, authorized, artifacts, decoded)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), admission.Instance.ID)
	mustProgress(t, err)
	for _, attempt := range attempts {
		if attempt.Status == store.GraphSucceeded && attempt.Node == "probe" {
			t.Fatalf("the agent attempt succeeded without a retained result: %+v", attempt)
		}
		outputs, e := f.db.ListGraphOutputsBounded(t.Context(), attempt.ID, 1<<16, 16)
		mustProgress(t, e)
		for _, output := range outputs {
			if output.Kind == store.GraphOutputJSON {
				t.Fatalf("output retained from terminal-only completion: %+v", attempt)
			}
		}
	}
	instance, err := f.db.GetGraphInstance(t.Context(), admission.Instance.ID)
	mustProgress(t, err)
	if instance.Status != store.GraphFailed || forge.attemptCount() != 0 {
		t.Fatalf("the graph advanced or published: %+v posts=%d", instance, forge.attemptCount())
	}
	comments, err := f.db.ListGraphComments(t.Context(), admission.Instance.ID, "", 10)
	mustProgress(t, err)
	if len(comments) != 0 {
		t.Fatalf("a comment intent was retained for unaccepted output: %+v", comments)
	}
}

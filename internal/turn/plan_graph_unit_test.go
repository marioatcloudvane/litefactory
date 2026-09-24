package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func planGraphTrustedContext() ar.Context {
	return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanGraph: &ar.PlanGraphBinding{Story: "story-001", Stage: "story", Candidate: "plan-11-22", RequestID: "plan-request-11-22", Upstream: []string{}, SharedRevision: "initial"}}
}

const planGraphAskEnvelope = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"The actor is unclear.","comment":"Please clarify the intended actor.","questions":[{"id":"q-1","audience":"business","text":"Who needs this?"}],"payload":{"plan":{}}}}`

func TestPlanRuntime_RejectsMalformedProviderOutput(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, raw string }{
		{"agent approval", strings.Replace(planGraphAskEnvelope, `"ask"`, `"approved"`, 1)},
		{"mode choice", strings.Replace(planGraphAskEnvelope, `"version":1`, `"version":1,"mode":"implement"`, 1)},
		{"parent authority", strings.Replace(planGraphAskEnvelope, `"version":1`, `"version":1,"parent_id":362`, 1)},
		{"owner authority", strings.Replace(planGraphAskEnvelope, `"version":1`, `"version":1,"owner_id":7`, 1)},
		{"duplicate response", strings.Replace(planGraphAskEnvelope, `"response":"ask"`, `"response":"ask","response":"done"`, 1)},
		{"missing result", `{"version":1,"response":"ask"}`},
		{"trailing payload", planGraphAskEnvelope + ` {"response":"approved"}`},
		{"oversized payload", strings.Repeat(" ", ar.MaxResearchNodeBytes) + planGraphAskEnvelope},
		{"earlier private format", `{"version":1,"response":"ask","request":null,"comment":"Please clarify the intended actor.","questions":["Who needs this?"]}`},
		{"earlier private review", `{"version":1,"response":"review_ready","request":null,"comment":"Review.","questions":[]}`},
		{"Research envelope", `{"schema_version":1,"outcome":"completed","research":{}}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if node, d := ar.DecodePlanNode([]byte(tt.raw), planGraphTrustedContext()); d.Valid {
				t.Fatalf("invalid output accepted: %+v", node)
			}
		})
	}
	if _, d := ar.DecodePlanNode([]byte(planGraphAskEnvelope), planGraphTrustedContext()); !d.Valid {
		t.Fatalf("a question under the shared contract was refused: %+v", d)
	}
}

func TestPlanRuntime_PolicyCarriesNoPrivateProtocol(t *testing.T) {
	t.Parallel()
	plan, research := PlanWorkflowPolicy(), ResearchWorkflowPolicy()
	if plan.Handlers != nil || research.Handlers != nil {
		t.Fatal("a static workflow policy carries private handlers")
	}
	if plan.Decode == nil || plan.Retain == nil || research.Decode == nil {
		t.Fatal("every workflow policy judges its result through the shared decoder")
	}
	if strings.Contains(plan.Guidance, "no shared application result wrapper") || !strings.Contains(plan.Guidance, "shared result helper") {
		t.Fatal("Plan guidance does not carry the shared route")
	}
	if got := (&planGraphRuntime{}).policy(); got.Handlers == nil || got.Decode == nil {
		t.Fatal("a running Plan policy keeps the shared decoder and adds only its handlers")
	}
}

func TestPlanRuntime_RetainedResultIsTheOnlySourceOfAskAndDone(t *testing.T) {
	t.Parallel()
	var ask ar.ResearchNode
	if err := json.Unmarshal([]byte(planGraphAskEnvelope), &ask); err != nil {
		t.Fatal(err)
	}
	got, err := planRetainedFrom("ask", ask.Result)
	if err != nil || got.Request != nil || got.Set != nil || len(got.Questions) != 1 || got.Questions[0] != "Who needs this?" || got.Comment != "Please clarify the intended actor." {
		t.Fatalf("question corrupted: %+v %v", got, err)
	}
	if _, err = planRetainedFrom("done", ask.Result); err == nil {
		t.Fatal("an incomplete result was read as a story effect")
	}
	if _, err = planRetainedFrom("approved", ask.Result); err == nil {
		t.Fatal("approval read as a Plan response")
	}
	done := ar.Result{Outcome: "completed", Comment: "Proposed.", Payload: ar.Payload{Plan: &ar.Plan{StoryRequest: &ar.PlanStoryRequest{Operation: "create", RequestID: "r", Stage: "story", Candidate: "c", SharedRevision: "s", Draft: planbrief.PlanDraft{Key: "story-001"}}}}}
	got, err = planRetainedFrom("done", done)
	if err != nil || got.Request == nil || got.Request.Candidate != "c" || got.Set != nil {
		t.Fatalf("done did not yield the recorded request: %+v %v", got, err)
	}
	done.Payload.Plan.StorySet = &ar.PlanStorySet{}
	if _, err = planRetainedFrom("done", done); err == nil {
		t.Fatal("both branches read as one")
	}
	if _, err = planRetainedFrom("ask", ar.Result{Outcome: "incomplete"}); err == nil {
		t.Fatal("a result with no Plan payload was read")
	}
}

func TestPlanRuntime_OldPrivateOutputIsNotAResultReference(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"private ask":     `{"version":1,"response":"ask","request":null,"comment":"Please clarify.","questions":["Who?"]}`,
		"private review":  `{"version":1,"response":"review_ready","request":{"operation":"create"},"comment":"Review.","questions":[]}`,
		"receipt":         `{"outcome":"applied"}`,
		"unknown members": `{"version":1,"kind":"shared_result","revision":1,"extra":true}`,
	} {
		if _, ok := decodePlanResultRef([]byte(raw)); ok {
			t.Fatalf("%s was read as a retained result reference", name)
		}
	}
	kept, err := retainPlanResult(store.ResultBinding{ResultID: 3, RunID: 5, Task: "acme/api#1", Mode: "plan", Attempt: "a"}, 2, ar.ResearchNode{Response: "done"})
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := decodePlanResultRef(kept)
	if !ok || ref.Revision != 2 || ref.Response != "done" || ref.Binding.RunID != 5 {
		t.Fatalf("a retained reference does not round trip: %+v", ref)
	}
}

func TestPlanRuntime_RejectsForeignCandidateBinding(t *testing.T) {
	t.Parallel()
	draft := func(key string) string {
		return strings.Replace(graphBindingDraft, `"key":"story-001"`, `"key":"`+key+`"`, 1)
	}
	envelope := func(member, key string) string {
		return `{"version":1,"response":"done","result":{"outcome":"completed","comment":"A usable draft.","payload":{"plan":{"storyrequest":{"operation":"create",` + member + `"draft":` + draft(key) + `}}}}}`
	}
	cases := []struct{ name, raw string }{
		{"wrong story", envelope("", "other-story")},
		{"supplied candidate", envelope(`"candidate":"plan-11-21",`, "story-001")},
		{"supplied stage", envelope(`"stage":"technical",`, "story-001")},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if node, d := ar.DecodePlanNode([]byte(tt.raw), planGraphTrustedContext()); d.Valid {
				t.Fatalf("foreign binding accepted: %+v", node)
			}
		})
	}
	if _, d := ar.DecodePlanNode([]byte(envelope("", "story-001")), planGraphTrustedContext()); !d.Valid {
		t.Fatalf("fixture invalid: %+v", d)
	}
}

func TestPlanRuntime_FactoryStageControls(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"story", "technical", "acceptance"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			r := planGraphRuntime{stage: stage}
			for _, condition := range []string{"story", "technical", "acceptance", "approved"} {
				got, err := r.control(context.Background(), Options{}, ActivityRequest{Attrs: map[string]string{"lf_condition": condition}})
				if err != nil {
					t.Fatal(err)
				}
				want := "false"
				if condition == stage {
					want = "true"
				}
				if got.Outcome != want {
					t.Fatalf("stage %q condition %q = %q, want %q", stage, condition, got.Outcome, want)
				}
			}
		})
	}
	if PlanWorkflowPolicy().Mode != agent.ModePlan || ResearchWorkflowPolicy().Mode != agent.ModeResearch {
		t.Fatal("workflow modes changed")
	}
}

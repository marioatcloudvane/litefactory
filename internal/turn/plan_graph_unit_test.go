package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestPlanRuntime_RejectsMalformedProviderOutput(t *testing.T) {
	t.Parallel()
	valid := `{"version":1,"response":"ask","request":null,"comment":"Please clarify the intended actor.","questions":["Who needs this?"]}`
	cases := []struct{ name, raw string }{
		{"agent approval", strings.Replace(valid, `"ask"`, `"approved"`, 1)},
		{"mode choice", strings.Replace(valid, `"version":1`, `"version":1,"mode":"implement"`, 1)},
		{"parent authority", strings.Replace(valid, `"version":1`, `"version":1,"parent_id":362`, 1)},
		{"owner authority", strings.Replace(valid, `"version":1`, `"version":1,"owner_id":7`, 1)},
		{"duplicate response", strings.Replace(valid, `"response":"ask"`, `"response":"ask","response":"review_ready"`, 1)},
		{"missing questions", strings.Replace(valid, `,"questions":["Who needs this?"]`, "", 1)},
		{"null questions", strings.Replace(valid, `["Who needs this?"]`, "null", 1)},
		{"trailing payload", valid + ` {"response":"approved"}`},
		{"oversized payload", strings.Repeat(" ", 128*1024) + valid},
		{"Research envelope", `{"schema_version":1,"outcome":"completed","research":{}}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := planGraphRuntime{selected: "story-001", stage: "story"}
			// No persistence or forge is available: rejected input must not reach effects.
			encoded, narrative, err := r.decode(context.Background(), ActivityRequest{InstanceID: 11, NodeAttemptID: 22}, []byte(tt.raw))
			if err == nil || len(encoded) != 0 || narrative != "" {
				t.Fatalf("invalid output retained: %s %q %v", encoded, narrative, err)
			}
		})
	}
}

func TestPlanRuntime_QuestionUsesDistinctProtocol(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"version":1,"response":"ask","request":null,"comment":"Please clarify the intended actor.","questions":["Who needs this?"]}`)
	r := planGraphRuntime{selected: "story-001", stage: "story"}
	encoded, narrative, err := r.decode(context.Background(), ActivityRequest{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	var node graphexec.PlanNode
	if err = json.Unmarshal(encoded, &node); err != nil {
		t.Fatal(err)
	}
	if node.Response != "ask" || node.Request != nil || len(node.Questions) != 1 || node.Questions[0] != "Who needs this?" || narrative != node.Comment {
		t.Fatalf("question corrupted: %#v %q", node, narrative)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 5 {
		t.Fatalf("unexpected envelope: %s", encoded)
	}
	if PlanWorkflowPolicy().NodeProtocol == nil || ResearchWorkflowPolicy().NodeProtocol != nil || ResearchWorkflowPolicy().Decode == nil {
		t.Fatal("Plan and Research protocol boundary lost")
	}
}

func TestPlanRuntime_RejectsForeignCandidateBinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*graphexec.PlanStoryRequest)
	}{
		{"wrong story", func(r *graphexec.PlanStoryRequest) { r.Draft.Key = "other-story" }},
		{"old attempt", func(r *graphexec.PlanStoryRequest) { r.Candidate = "plan-11-21" }},
		{"different graph", func(r *graphexec.PlanStoryRequest) { r.Candidate = "plan-12-22" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node := graphexec.PlanNode{Version: 1, Response: "review_ready", Comment: "A usable draft.", Questions: []string{}, Request: &graphexec.PlanStoryRequest{RequestID: "request-1", Operation: "create", Stage: "story", Candidate: "plan-11-22", Upstream: []string{}, SharedRevision: "initial", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Useful planning", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "a guided plan", Benefit: "clear decisions"}, Problem: "Avoid losing decisions.", InScope: []string{"Planning"}, NotInScope: []string{"Implementation"}}}}
			tt.change(node.Request)
			raw, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			// Establish payload validity independently; this test targets runtime binding.
			if _, err = graphexec.ParsePlanNode(raw, planbrief.ContentScope{}); err != nil {
				t.Fatalf("fixture invalid: %v", err)
			}
			r := planGraphRuntime{selected: "story-001", stage: "story"}
			encoded, narrative, err := r.decode(context.Background(), ActivityRequest{InstanceID: 11, NodeAttemptID: 22}, raw)
			if err == nil || !strings.Contains(err.Error(), "factory story/stage/candidate binding") || len(encoded) != 0 || narrative != "" {
				t.Fatalf("foreign binding retained: %s %q %v", encoded, narrative, err)
			}
		})
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

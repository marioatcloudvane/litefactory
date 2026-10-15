package teamsource

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestWorkflowJudgeResourceCapture(t *testing.T) {
	source := researchTestSource()
	replaceResearchFixture(source, "research.dot", `work -> end [lf_on="success"];`, `work -> challenge [lf_on="success"];
 challenge [lf_action="lf_judge",lf_source="work",lf_limit="1",lf_outcomes="success,failed"];
 challenge -> end [lf_on="success"];
 challenge -> end [lf_on="failed"];`)
	registry := researchTestRegistry(t)
	if err := registry.Register(graphexec.JudgeSpec(agent.ModeResearch)); err != nil {
		t.Fatal(err)
	}
	def, err := CaptureWorkflow(t.Context(), source, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	role, err := WorkflowJudgeInstructions(def)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := JudgeInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(role, shipped) || def.Hashes["judge/challenger.md"] == "" {
		t.Fatal("judge role absent from captured identity")
	}
	retained := RetainedFiles(def.Files)
	retained["judge/challenger.md"] = []byte("previously captured challenger")
	replay, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	role, err = WorkflowJudgeInstructions(replay)
	if err != nil || string(role) != "previously captured challenger" {
		t.Fatalf("replay changed role: %s, %v", role, err)
	}
	delete(retained, "judge/challenger.md")
	if _, err = CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry); err == nil {
		t.Fatal("missing retained instructions silently replaced")
	}
}

// The full shipped graphs remain unchanged; authors can add report-only steps
// while keeping their existing source decisions and owner gates.
func TestWorkflowJudgeControlGraphAdmission(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan} {
		t.Run(string(mode), func(t *testing.T) {
			var assets fs.FS
			var policy graphexec.ControlPolicy
			var check func(*graphexec.Profile) error
			sourceNode := "investigate"
			if mode == agent.ModeResearch {
				assets = researchAssets
				policy = graphexec.ResearchControlPolicy()
				check = graphexec.ValidateResearchControlGraph
			} else {
				assets = planAssets
				policy = graphexec.PlanControlPolicy()
				check = graphexec.ValidatePlanControlGraph
				sourceNode = "technical_agent"
			}
			raw, err := fs.ReadFile(assets, string(mode)+"/"+string(mode)+".dot")
			if err != nil {
				t.Fatal(err)
			}
			profile, err := graphexec.ParseProfile(raw)
			if err != nil {
				t.Fatal(err)
			}
			registry := graphexec.Registry{Policy: check}
			specs := []graphexec.ActionSpec{
				{Name: "lf_agent", Mode: mode, Outcomes: []string{"success", "failed"}, Attributes: graphexec.AgentAttributes()},
				graphexec.ControlSpec("lf_loop", policy), graphexec.ControlSpec("lf_if", policy), graphexec.JudgeSpec(mode),
				{Name: "lf_ask", Mode: mode, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question", "review"}}}},
				{Name: "lf_comment", Mode: mode, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"final", "failure", "advisory"}}}},
			}
			if mode == agent.ModePlan {
				specs = append(specs, graphexec.PlanStorySpec())
			} else {
				specs = append(specs, graphexec.ActionSpec{Name: "lf_research_brief", Mode: mode, Outcomes: []string{"success", "failed"}})
			}
			for _, spec := range specs {
				if err := registry.Register(spec); err != nil {
					t.Fatal(err)
				}
			}
			if err := graphexec.Validate(profile, registry); err != nil {
				t.Fatalf("shipped control: %v", err)
			}
			original := ""
			for i, e := range profile.Edges {
				if e.From == sourceNode && e.On == "success" {
					original = e.To
					profile.Edges[i].To = "challenge"
				}
			}
			if original == "" {
				t.Fatal("missing source success edge")
			}
			profile.Nodes = append(profile.Nodes,
				graphexec.Node{Name: "challenge", Action: graphexec.JudgeAction, Limit: 8, Outcomes: []string{"success", "failed"}, Attrs: map[string]string{"lf_source": sourceNode, "lf_agent": "other"}},
				graphexec.Node{Name: "advice", Action: "lf_comment", Limit: 8, Outcomes: []string{"published"}, Attrs: map[string]string{"lf_source": "challenge", "lf_kind": "advisory"}},
			)
			profile.Edges = append(profile.Edges, graphexec.Edge{From: "challenge", To: "advice", On: "success"}, graphexec.Edge{From: "challenge", To: "advice", On: "failed"}, graphexec.Edge{From: "advice", To: original, On: "published"})
			if err := graphexec.Validate(profile, registry); err != nil {
				t.Fatalf("opt-in control graph: %v", err)
			}
			for i := range profile.Edges {
				if profile.Edges[i].From == "advice" {
					profile.Edges[i].To = "promoted"
					if mode == agent.ModeResearch {
						profile.Edges[i].To = "end"
					}
				}
			}
			if err := graphexec.Validate(profile, registry); err == nil {
				t.Fatal("advisory publication bypassed required source publication or owner gate")
			}
		})
	}
}

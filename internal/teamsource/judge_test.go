package teamsource

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
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
	replay, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Digest != def.Digest {
		t.Fatal("retained identity changed")
	}
	retained[judgeLegacyRolePath] = []byte("damaged role")
	if _, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry); err == nil {
		t.Fatal("damaged role accepted")
	}
	// Historical captures keep their exact identity but cannot launch again.
	for name := range retained {
		if strings.HasPrefix(name, "judge/") {
			delete(retained, name)
		}
	}
	retained[judgeLegacyRolePath] = []byte("previously captured challenger")
	replay, err = CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	role, err = WorkflowJudgeInstructions(replay)
	if err != nil || string(role) != "previously captured challenger" {
		t.Fatalf("historical role changed: %s, %v", role, err)
	}
	if _, err := WorkflowJudgeCapture(replay); err == nil {
		t.Fatal("historical capture allowed launch")
	}
	delete(retained, judgeLegacyRolePath)
	if _, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry); err == nil {
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
				assets = RetainedFiles(teamFiles(t, goTeam))
				policy = graphexec.ResearchControlPolicy()
				check = graphexec.ValidateResearchControlGraph
			} else {
				assets = RetainedFiles(teamFiles(t, goTeam))
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

func TestFreshJudgeCaptureFromRootRetainedFiles(t *testing.T) {
	for _, withJudge := range []bool{false, true} {
		name := "without judge"
		if withJudge {
			name = "with judge"
		}
		t.Run(name, func(t *testing.T) {
			fixture := researchTestSource()
			registry := researchTestRegistry(t)
			if withJudge {
				replaceResearchFixture(fixture, "research.dot", `work -> end [lf_on="success"];`, `work -> challenge [lf_on="success"];
 challenge [lf_action="lf_judge",lf_source="work",lf_limit="1",lf_outcomes="success,failed"];
 challenge -> end [lf_on="success"];
 challenge -> end [lf_on="failed"];`)
				if err := registry.Register(graphexec.JudgeSpec(agent.ModeResearch)); err != nil {
					t.Fatal(err)
				}
			}
			source := RetainedFiles{}
			for path, file := range fixture {
				source[path] = bytes.Clone(file.Data)
			}
			root, err := fs.Sub(source, ".")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := root.(RetainedFiles); !ok {
				t.Fatal("fixture does not preserve root retained filesystem type")
			}
			paths := config.NewPaths(t.TempDir())
			if withJudge {
				if _, err := CaptureWorkflowIdentity(t.Context(), root, "research.dot", "builtin", agent.WorkflowResearch, registry, paths); err == nil {
					t.Fatal("retained source without judge closure accepted as fresh")
				}
			}
			freshSource := struct{ fs.FS }{root}
			fresh, err := CaptureWorkflowIdentity(t.Context(), freshSource, "research.dot", "builtin", agent.WorkflowResearch, registry, paths)
			if err != nil {
				t.Fatalf("fresh root capture: %v", err)
			}
			if withJudge {
				if _, err := WorkflowJudgeCapture(fresh); err != nil {
					t.Fatalf("fresh judge closure: %v", err)
				}
			} else {
				for path := range fresh.Files {
					if strings.HasPrefix(path, "judge/") {
						t.Fatalf("nonjudge workflow captured %s", path)
					}
				}
			}
			restored, err := CaptureWorkflowIdentity(t.Context(), RetainedFiles(fresh.Files), "research.dot", "builtin", agent.WorkflowResearch, registry)
			if err != nil {
				t.Fatalf("restored root capture: %v", err)
			}
			if fresh.Digest != restored.Digest {
				t.Fatal("restore changed root capture identity")
			}
		})
	}
}

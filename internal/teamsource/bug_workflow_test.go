package teamsource

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"strings"
	"testing"
)

func TestBugWorkflow_CaptureOwnership(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, declaration, team string
		owner                   agent.Workflow
		valid                   bool
	}{
		{"standalone", "workflow=bug", "builtin", agent.WorkflowBug, true},
		{"conflicting mode", "workflow=bug,mode=research", "builtin", agent.WorkflowBug, false},
		{"foreign package", "workflow=bug", "other", agent.WorkflowBug, false},
		{"foreign workflow", "workflow=bug", "builtin", agent.WorkflowPlan, false},
		{"mode surrogate", "mode=bug", "builtin", agent.WorkflowBug, false},
		{"unknown workflow", "workflow=unknown", "builtin", agent.Workflow("unknown"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := researchTestSource()
			source["research.dot"].Data = []byte(strings.ReplaceAll(string(source["research.dot"].Data), `mode="research"`, tt.declaration))
			r := graphexec.Registry{Policy: func(p *graphexec.Profile) error {
				if p.Workflow != agent.WorkflowBug || p.Mode != "" {
					return fmt.Errorf("mismatched Bug policy owner")
				}
				return nil
			}}
			if err := r.Register(graphexec.ActionSpec{Name: "lf_agent", Workflow: agent.WorkflowBug, Outcomes: []string{"success"}}); err != nil {
				t.Fatal(err)
			}
			got, err := CaptureWorkflowIdentity(t.Context(), source, "research.dot", tt.team, tt.owner, r)
			if (err == nil) != tt.valid {
				t.Fatalf("capture=%v", err)
			}
			if tt.valid {
				if got.Profile.Mode != "" || got.Profile.Workflow != agent.WorkflowBug {
					t.Fatalf("identity=%+v", got.Profile)
				}
				if len(got.Files) != 5 || len(got.Nodes) != 1 || got.Digest == "" {
					t.Fatalf("incomplete capture: files=%d nodes=%d digest=%q", len(got.Files), len(got.Nodes), got.Digest)
				}
			}
		})
	}
}

func TestBugWorkflow_CompatibilityCapturePreservesPins(t *testing.T) {
	t.Parallel()
	source := researchTestSource()
	registry := researchTestRegistry(t)
	old, err := CaptureWorkflow(t.Context(), source, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	current, err := CaptureWorkflowIdentity(t.Context(), source, "research.dot", "builtin", agent.WorkflowResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	if old.Digest != current.Digest || old.Profile.Digest != current.Profile.Digest || string(old.Files["research.dot"]) != string(current.Files["research.dot"]) {
		t.Fatal("compatibility mapping rewrote captured definition or digest")
	}
	if _, err := CaptureWorkflow(t.Context(), source, "research.dot", "builtin", agent.Mode("bug"), registry); err == nil {
		t.Fatal("mode API accepted bug")
	}
}

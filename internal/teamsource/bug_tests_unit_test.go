package teamsource

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func bugTestsUnitProfile() *graphexec.Profile {
	p := &graphexec.Profile{Workflow: agent.WorkflowBug, Team: "builtin", ProfileVersion: 1, Start: "regression"}
	phases := []string{"regression", "focused", "full"}
	for i, phase := range phases {
		p.Nodes = append(p.Nodes, graphexec.Node{Name: phase, Action: "lf_bug_checks", Outcomes: []string{"passed", "failed", "blocked", "cancelled"}, Limit: 1, Attrs: map[string]string{"lf_phase": phase}})
		next := "passed"
		if i+1 < len(phases) {
			next = phases[i+1]
		}
		p.Edges = append(p.Edges, graphexec.Edge{From: phase, To: next, On: "passed"})
		for _, outcome := range []string{"failed", "blocked", "cancelled"} {
			p.Edges = append(p.Edges, graphexec.Edge{From: phase, To: outcome, On: outcome})
		}
	}
	for _, outcome := range []string{"passed", "failed", "blocked", "cancelled"} {
		p.Nodes = append(p.Nodes, graphexec.Node{Name: outcome, Terminal: true})
	}
	return p
}

func TestValidateBugTests_ChildBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*graphexec.Profile)
	}{
		{"foreign workflow", func(p *graphexec.Profile) { p.Workflow = agent.WorkflowResearch }},
		{"mode surrogate", func(p *graphexec.Profile) { p.Mode = agent.ModeResearch }},
		{"foreign package", func(p *graphexec.Profile) { p.Team = "other" }},
		{"graph permissions", func(p *graphexec.Profile) { p.Attrs = map[string]string{"permissions": "write"} }},
		{"phase role", func(p *graphexec.Profile) { p.Nodes[0].Attrs["lf_role"] = "bug-fixer" }},
		{"phase command", func(p *graphexec.Profile) { p.Nodes[0].Attrs["command"] = "true" }},
		{"edge permission", func(p *graphexec.Profile) { p.Edges[0].Attrs = map[string]string{"permissions": "write"} }},
		{"skip regression", func(p *graphexec.Profile) { p.Start = "focused" }},
		{"missing phase", func(p *graphexec.Profile) { p.Nodes = p.Nodes[1:] }},
		{"extra node", func(p *graphexec.Profile) { p.Nodes = append(p.Nodes, graphexec.Node{Name: "extra", Terminal: true}) }},
		{"missing outcome", func(p *graphexec.Profile) { p.Nodes[0].Outcomes = p.Nodes[0].Outcomes[:3] }},
		{"unknown outcome", func(p *graphexec.Profile) { p.Nodes[0].Outcomes[3] = "unknown" }},
		{"missing transition", func(p *graphexec.Profile) { p.Edges = p.Edges[1:] }},
		{"unknown transition", func(p *graphexec.Profile) { p.Edges[0].On = "unknown" }},
		{"skip focused", func(p *graphexec.Profile) { p.Edges[0].To = "full" }},
		{"cycle", func(p *graphexec.Profile) { p.Edges[8].To = "regression" }},
		{"failed advances", func(p *graphexec.Profile) { p.Edges[1].To = "focused" }},
		{"blocked advances", func(p *graphexec.Profile) { p.Edges[2].To = "focused" }},
		{"cancelled advances", func(p *graphexec.Profile) { p.Edges[3].To = "focused" }},
		{"terminal executes", func(p *graphexec.Profile) { p.Nodes[3].Action = "lf_bug_merge" }},
		{"ambiguous duplicate edge", func(p *graphexec.Profile) { p.Edges[1] = p.Edges[0] }},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/refused", func(t *testing.T) {
			t.Parallel()
			p := bugTestsUnitProfile()
			tt.mutate(p)
			if err := ValidateBugTests(p); err == nil {
				t.Fatal("unsafe child definition accepted")
			}
		})
	}
	t.Run("complete sequential graph/admitted", func(t *testing.T) {
		t.Parallel()
		if err := ValidateBugTests(bugTestsUnitProfile()); err != nil {
			t.Fatalf("valid sequential child refused: %v", err)
		}
	})
	t.Run("missing definition/refused", func(t *testing.T) {
		t.Parallel()
		if err := ValidateBugTests(nil); err == nil {
			t.Fatal("nil child definition accepted")
		}
	})
}

func TestValidateBugTests_PhaseCapabilities(t *testing.T) {
	t.Parallel()
	for i, phase := range []string{"regression", "focused", "full"} {
		for _, action := range []string{"lf_agent", "lf_bug_test_graph", "lf_bug_pr", "lf_bug_merge", "lf_bug_close", "lf_bug_attempt", "lf_comment", "lf_unknown"} {
			t.Run(phase+"/"+action+"/refused", func(t *testing.T) {
				t.Parallel()
				p := bugTestsUnitProfile()
				p.Nodes[i].Action = action
				if err := ValidateBugTests(p); err == nil {
					t.Fatal("child gained a forbidden capability")
				}
			})
		}
		for _, limit := range []int{-1, 0, 2} {
			t.Run(phase+"/invalid limit", func(t *testing.T) {
				t.Parallel()
				p := bugTestsUnitProfile()
				p.Nodes[i].Limit = limit
				if err := ValidateBugTests(p); err == nil {
					t.Fatal("phase repetition or unbounded dispatch admitted")
				}
			})
		}
	}
}

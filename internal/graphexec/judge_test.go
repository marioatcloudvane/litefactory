package graphexec

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestJudgeExplicitBinding(t *testing.T) {
	p := &Profile{Mode: agent.ModeResearch, Nodes: []Node{
		{Name: "source", Action: "lf_agent"},
		{Name: "judge", Action: JudgeAction, Limit: 1, Outcomes: []string{"success", "failed"}, Attrs: map[string]string{"lf_source": "source", "lf_agent": "other", "lf_budget": "2"}},
		{Name: "publish", Action: "lf_comment", Outcomes: []string{"published"}, Attrs: map[string]string{"lf_kind": "advisory", "lf_source": "judge"}},
	}}
	if err := ValidateJudgeGraph(p); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Profile)
	}{
		{"implicit source", func(p *Profile) { delete(p.Nodes[1].Attrs, "lf_source") }},
		{"wrong source", func(p *Profile) { p.Nodes[1].Attrs["lf_source"] = "publish" }},
		{"unbounded", func(p *Profile) { p.Nodes[1].Limit = 0 }},
		{"write authority", func(p *Profile) { p.Nodes[1].Attrs["lf_access"] = "write" }},
		{"empty settings", func(p *Profile) { p.Nodes[1].Attrs["lf_model"] = " " }},
		{"bad budget", func(p *Profile) { p.Nodes[1].Attrs["lf_budget"] = "61" }},
		{"judge owner wait", func(p *Profile) { p.Nodes[2].Action = "lf_ask" }},
		{"wrong report", func(p *Profile) { p.Nodes[2].Attrs["lf_source"] = "source" }},
		{"unsupported workflow", func(p *Profile) { p.Mode = agent.ModeImplement }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *p
			candidate.Nodes = append([]Node(nil), p.Nodes...)
			for i := range candidate.Nodes {
				candidate.Nodes[i].Attrs = map[string]string{}
				for k, v := range p.Nodes[i].Attrs {
					candidate.Nodes[i].Attrs[k] = v
				}
			}
			tc.mutate(&candidate)
			if ValidateJudgeGraph(&candidate) == nil {
				t.Fatal("invalid judge contract admitted")
			}
		})
	}
}

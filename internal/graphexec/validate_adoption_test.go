package graphexec_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

func TestValidateAdoptionGraphBoundary(t *testing.T) {
	t.Parallel()
	files, err := teamsourcetest.Files("go-development")
	if err != nil {
		t.Fatal(err)
	}
	raw := files["validate/validate.dot"]
	for _, tt := range []struct {
		name   string
		mutate func(*graphexec.Profile)
		want   string
	}{
		{"declared adoption", func(*graphexec.Profile) {}, ""},
		{"only owner resolution can enter", func(p *graphexec.Profile) {
			p.Edges = append(p.Edges, graphexec.Edge{From: "repair_check", To: "owner_repair_apply", On: "too_big"})
		}, "may only be reached by owner resolution"},
		{"suite required", func(p *graphexec.Profile) {
			for i := range p.Edges {
				if p.Edges[i].From == "owner_repair_apply" && p.Edges[i].On == "applied" {
					p.Edges[i].To = "owner_report"
				}
			}
		}, "must run the suite"},
		{"blocked must report", func(p *graphexec.Profile) {
			for i := range p.Edges {
				if p.Edges[i].From == "owner_repair_apply" && p.Edges[i].On == "blocked" {
					p.Edges[i].To = "suite"
				}
			}
		}, "must report the blocked adoption"},
		{"one apply node", func(p *graphexec.Profile) {
			n, _ := p.Node("owner_repair_apply")
			n.Name = "second_apply"
			p.Nodes = append(p.Nodes, n)
		}, "at most one lf_validate_repair_apply"},
		{"historical graph remains valid", func(p *graphexec.Profile) {
			p.Nodes = slices.DeleteFunc(p.Nodes, func(n graphexec.Node) bool { return n.Action == graphexec.ValidateRepairApplyAction })
			p.Edges = slices.DeleteFunc(p.Edges, func(e graphexec.Edge) bool { return e.From == "owner_repair_apply" || e.To == "owner_repair_apply" })
			for i := range p.Nodes {
				if p.Nodes[i].Action == graphexec.ValidateResolveAction {
					p.Nodes[i].Outcomes = slices.DeleteFunc(p.Nodes[i].Outcomes, func(s string) bool { return s == "apply_repair" })
				}
			}
		}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := graphexec.ParseProfile(raw)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(p)
			err = graphexec.ValidateFlowGraph(p)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v; want %q", err, tt.want)
			}
		})
	}
}

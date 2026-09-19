package graphexec_test

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func registry(t *testing.T) graphexec.Registry {
	t.Helper()
	var r graphexec.Registry
	if err := r.Register(graphexec.ActionSpec{Name: "lf_implement_work", Mode: agent.Mode("implement"), Outcomes: []string{"done", "again", "failed"}}); err != nil {
		t.Fatal(err)
	}
	return r
}

func profile() *graphexec.Profile {
	return &graphexec.Profile{Team: "test", Mode: agent.Mode("implement"), ProfileVersion: 1, Start: "work", Nodes: []graphexec.Node{{Name: "work", Action: "lf_implement_work", Outcomes: []string{"done"}, Attrs: map[string]string{"lf_custom": "raw"}}, {Name: "end", Terminal: true}}, Edges: []graphexec.Edge{{From: "work", To: "end", On: "done"}}}
}

func TestParseProfilePreservesAttributes(t *testing.T) {
	t.Parallel()
	input := []byte(`digraph example {
 team="test"; graph [mode="implement", profile_version="1", start="work"];
 node [lf_custom="default", color="blue"];
 edge [on="done", lf_edge="unchanged"];
 work [lf_action="lf_implement_work", lf_outcomes="done", lf_custom="$(literal)\\n", lf_agent_markdown="test/implement/dev.md", lf_model_tier="balanced"];
 end [lf_terminal="true"];
 work -> end [label="finish"];
 }`)
	p, err := graphexec.ParseProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	if p.Team != "test" || p.Mode != "implement" || p.ProfileVersion != 1 || p.Start != "work" {
		t.Fatalf("graph attrs: %+v", p)
	}
	if p.Attrs["team"] != "test" || p.Attrs["start"] != "work" || p.Attrs["profile_version"] != "1" || p.Attrs["mode"] != "implement" {
		t.Fatalf("raw graph attrs: %v", p.Attrs)
	}
	if len(p.Nodes) != 2 || len(p.Edges) != 1 {
		t.Fatalf("nodes/edges: %+v", p)
	}
	n, ok := p.Node("work")
	if !ok {
		t.Fatal("missing work")
	}
	if n.Action != "lf_implement_work" || !reflect.DeepEqual(n.Outcomes, []string{"done"}) || n.Attrs["color"] != "blue" || n.Attrs["lf_custom"] != `$(literal)\\n` {
		t.Fatalf("node attrs: %+v", n)
	}
	if p.Nodes[1].Attrs["lf_custom"] != "default" || !p.Nodes[1].Terminal {
		t.Fatalf("default/terminal: %+v", p.Nodes[1])
	}
	e := p.Edges[0]
	if e.From != "work" || e.To != "end" || e.On != "done" || e.Attrs["lf_edge"] != "unchanged" || e.Attrs["label"] != "finish" {
		t.Fatalf("edge: %+v", e)
	}
	if p.Digest != fmt.Sprintf("%x", sha256.Sum256(input)) {
		t.Fatalf("digest %q", p.Digest)
	}
	other, err := graphexec.ParseProfile(append(append([]byte(nil), input...), '\n'))
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest == p.Digest {
		t.Fatal("digest ignored exact bytes")
	}
	if err := graphexec.Validate(p, registry(t)); err != nil {
		t.Fatal(err)
	}
}

func TestParseProfileMalformed(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "digraph {", "graph { a -- b }", "digraph {} digraph {}", `digraph {profile_version="wrong"}`, `digraph {a [lf_limit="x"]}`, `digraph {a [lf_terminal="x"]}`} {
		t.Run(input, func(t *testing.T) {
			if _, err := graphexec.ParseProfile([]byte(input)); err == nil {
				t.Fatal("accepted malformed profile")
			}
		})
	}
}

func TestValidateFaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*graphexec.Profile)
		want   string
	}{
		{"unknown action", func(p *graphexec.Profile) { p.Nodes[0].Action = "lf_implement_unknown" }, "unknown action"},
		{"outside outcomes", func(p *graphexec.Profile) { p.Nodes[0].Outcomes = []string{"other"} }, "outside"},
		{"zero edges", func(p *graphexec.Profile) { p.Edges = nil }, "0 edges"},
		{"two edges", func(p *graphexec.Profile) { p.Edges = append(p.Edges, p.Edges[0]) }, "2 edges"},
		{"unreachable node", func(p *graphexec.Profile) {
			p.Nodes = append(p.Nodes, graphexec.Node{Name: "lost", Action: "lf_implement_work", Outcomes: []string{"done"}})
			p.Edges = append(p.Edges, graphexec.Edge{From: "lost", To: "end", On: "done"})
		}, "unreachable node"},
		{"unreachable terminal", func(p *graphexec.Profile) { p.Nodes = append(p.Nodes, graphexec.Node{Name: "lost", Terminal: true}) }, "unreachable terminal"},
		{"missing terminal", func(p *graphexec.Profile) { p.Nodes = p.Nodes[:1] }, "no terminal"},
		{"cycle without limit", func(p *graphexec.Profile) {
			p.Nodes[0].Outcomes = append(p.Nodes[0].Outcomes, "again")
			p.Edges = append(p.Edges, graphexec.Edge{From: "work", To: "work", On: "again"})
		}, "cycle without"},
		{"duplicate name", func(p *graphexec.Profile) { p.Nodes = append(p.Nodes, p.Nodes[0]) }, "duplicate node"},
		{"undeclared target", func(p *graphexec.Profile) { p.Edges[0].To = "missing" }, "to undeclared"},
		{"invalid tier", func(p *graphexec.Profile) { p.Nodes[0].Attrs["lf_model_tier"] = "vendor" }, "lf_model_tier"},
		{"mode mismatch", func(p *graphexec.Profile) { p.Mode = "research" }, "does not match"},
		{"terminal action", func(p *graphexec.Profile) { p.Nodes[1].Action = "lf_implement_work" }, "must not have action"},
		{"terminal outgoing", func(p *graphexec.Profile) {
			p.Edges = append(p.Edges, graphexec.Edge{From: "end", To: "work", On: "done"})
		}, "outgoing edge"},
		{"duplicate outcome", func(p *graphexec.Profile) { p.Nodes[0].Outcomes = append(p.Nodes[0].Outcomes, "done") }, "duplicate outcome"},
		{"empty outcomes", func(p *graphexec.Profile) { p.Nodes[0].Outcomes = nil }, "nonempty"},
		{"bad version", func(p *graphexec.Profile) { p.ProfileVersion = 0 }, "profile_version"},
		{"missing start", func(p *graphexec.Profile) { p.Start = "missing" }, "start node"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := profile()
			tt.change(p)
			r := registry(t)
			err := graphexec.Validate(p, r)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q; got %v", tt.want, err)
			}
			if next := graphexec.Validate(p, r); next.Error() != err.Error() {
				t.Fatal("nondeterministic diagnostics")
			}
		})
	}
}

func TestValidateAggregatesFaults(t *testing.T) {
	p := profile()
	p.Nodes[0].Action = "unknown"
	p.Nodes[0].Attrs["lf_model_tier"] = "invalid"
	p.Edges = nil
	err := graphexec.Validate(p, registry(t))
	if err == nil {
		t.Fatal("missing error")
	}
	for _, want := range []string{"unknown action", "lf_model_tier", "0 edges", "unreachable terminal"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestBoundedCyclesAndDecide(t *testing.T) {
	t.Parallel()
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprint(multi), func(t *testing.T) {
			p := profile()
			p.Nodes[0].Limit = 3
			p.Nodes[0].Outcomes = append(p.Nodes[0].Outcomes, "again")
			target := "work"
			if multi {
				target = "second"
				p.Nodes = append(p.Nodes, graphexec.Node{Name: "second", Action: "lf_implement_work", Outcomes: []string{"again"}, Limit: 2})
				p.Edges = append(p.Edges, graphexec.Edge{From: "second", To: "work", On: "again"})
			}
			p.Edges = append(p.Edges, graphexec.Edge{From: "work", To: target, On: "again"})
			if err := graphexec.Validate(p, registry(t)); err != nil {
				t.Fatal(err)
			}
			for _, e := range p.Edges {
				for range 3 {
					got, ok := graphexec.Decide(p, e.From, e.On)
					if !ok || got != e.To {
						t.Fatalf("Decide(%s,%s)=%q,%v", e.From, e.On, got, ok)
					}
				}
			}
			for _, n := range p.Nodes {
				if got, ok := graphexec.Decide(p, n.Name, "undeclared"); ok || got != "" {
					t.Fatalf("undeclared decision %q %v", got, ok)
				}
			}
			if got, ok := graphexec.Decide(p, "missing", "done"); ok || got != "" {
				t.Fatal("missing node accepted")
			}
			if multi {
				p.Nodes[2].Limit = 0
				if err := graphexec.Validate(p, registry(t)); err == nil {
					t.Fatal("unbounded second cycle member accepted")
				}
			}
		})
	}
}

func TestInvokeAgentConfig(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"flagship", "balanced", "fast"} {
		t.Run(tier, func(t *testing.T) {
			got, err := graphexec.DecodeInvokeAgentConfig(map[string]string{"lf_agent_markdown": "team/implement/agent.md", "lf_model_tier": tier})
			if err != nil || got.AgentMarkdownRef != "team/implement/agent.md" || string(got.ModelTier) != tier {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
	for _, tier := range []string{"", "vendor", "FAST"} {
		t.Run("invalid tier "+tier, func(t *testing.T) {
			_, err := graphexec.DecodeInvokeAgentConfig(map[string]string{"lf_agent_markdown": "team/a.md", "lf_model_tier": tier})
			if err == nil {
				t.Fatal("accepted tier")
			}
			for _, want := range []string{"lf_model_tier", fmt.Sprintf("%q", tier), "flagship", "balanced", "fast"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("missing %q: %v", want, err)
				}
			}
		})
	}
	for _, path := range []string{"", "/root/a.md", "C:/a.md", "C:a.md", `team\a.md`, "./a.md", "team/../a.md", "team/./a.md", "team//a.md", "team/"} {
		t.Run("invalid path "+path, func(t *testing.T) {
			_, err := graphexec.DecodeInvokeAgentConfig(map[string]string{"lf_agent_markdown": path, "lf_model_tier": "fast"})
			if err == nil || !strings.Contains(err.Error(), "lf_agent_markdown") {
				t.Fatalf("path %q: %v", path, err)
			}
		})
	}
}

func TestOwnershipBoundaries(t *testing.T) {
	r := registry(t)
	spec, ok := r.Resolve("lf_implement_work")
	if !ok {
		t.Fatal("missing action")
	}
	spec.Outcomes[0] = "changed"
	again, _ := r.Resolve(spec.Name)
	if again.Outcomes[0] != "done" {
		t.Fatal("Resolve leaked slice")
	}
	original := graphexec.ActionSpec{Name: "lf_implement_other", Mode: "implement", Outcomes: []string{"ok"}}
	if err := r.Register(original); err != nil {
		t.Fatal(err)
	}
	original.Outcomes[0] = "changed"
	again, _ = r.Resolve(original.Name)
	if again.Outcomes[0] != "ok" {
		t.Fatal("Register retained caller slice")
	}
	p := profile()
	n, ok := p.Node("work")
	if !ok {
		t.Fatal("missing node")
	}
	n.Outcomes[0] = "changed"
	n.Attrs["lf_custom"] = "changed"
	if p.Nodes[0].Outcomes[0] != "done" || p.Nodes[0].Attrs["lf_custom"] != "raw" {
		t.Fatal("Node leaked mutable values")
	}
	if _, ok := r.Resolve("unknown"); ok {
		t.Fatal("unknown resolved")
	}
	if err := r.Register(spec); err == nil {
		t.Fatal("duplicate registration accepted")
	}
}

func TestRegistryRejectsInvalidSpecs(t *testing.T) {
	for _, spec := range []graphexec.ActionSpec{{Name: "bad", Mode: "implement", Outcomes: []string{"ok"}}, {Name: "lf_research_work", Mode: "implement", Outcomes: []string{"ok"}}, {Name: "lf_implement_work", Mode: "implement"}, {Name: "lf_implement_work", Mode: "implement", Outcomes: []string{"ok", "ok"}}, {Name: "lf_implement_work", Mode: "implement", Outcomes: []string{""}}} {
		t.Run(fmt.Sprint(spec), func(t *testing.T) {
			var r graphexec.Registry
			if err := r.Register(spec); err == nil {
				t.Fatal("accepted invalid spec")
			}
		})
	}
}

func TestParseProfileRetainsInvalidIdentitiesForValidation(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		nodes            int
	}{
		{"duplicate declaration", `work [lf_action="lf_implement_work",lf_outcomes="done"]; work [lf_action="lf_implement_work",lf_outcomes="done"]; end [lf_terminal=true]; work -> end [on=done];`, "duplicate node", 3},
		{"undeclared endpoint", `work [lf_action="lf_implement_work",lf_outcomes="done"]; end [lf_terminal=true]; work -> missing [on=done];`, "to undeclared", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := graphexec.ParseProfile([]byte(`digraph {team=test;mode=implement;profile_version=1;start=work;` + tt.body + `}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Nodes) != tt.nodes {
				t.Fatalf("want %d declared nodes, got %d", tt.nodes, len(p.Nodes))
			}
			err = graphexec.Validate(p, registry(t))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

package graph

import (
	"strings"
	"testing"
)

func TestParseRetainsDeclarationsAndDOTStrings(t *testing.T) {
	d, err := Parse([]byte(`digraph { start=one; node [tag="old",tag="new"]; one [text="a\b\"c"]; one [tag="again"]; edge [choice="ok"]; one -> two -> end; two; end; }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Declarations) != 9 {
		t.Fatalf("declarations: %+v", d.Declarations)
	}
	defaults := d.Declarations[1]
	if len(defaults.Raw) != 2 || defaults.Raw[0].Value != "old" || defaults.Raw[1].Value != "new" {
		t.Fatalf("raw defaults: %+v", defaults)
	}
	if d.Declarations[2].Attrs["tag"] != "new" || d.Declarations[2].Attrs["text"] != `a\b"c` {
		t.Fatalf("effective defaults or escapes: %+v", d.Declarations[2])
	}
	if d.Declarations[5].From != "one" || d.Declarations[5].To != "two" || d.Declarations[6].From != "two" || d.Declarations[6].To != "end" {
		t.Fatalf("edge chain: %+v", d.Declarations)
	}
}

func TestStructuralValidationAndNavigation(t *testing.T) {
	var r Registry
	spec := ActionSpec{Name: "consumer", Outcomes: []string{"done", "wait"}, Attributes: map[string]AttributeSpec{"flavors": {Required: true, List: true, Values: []string{"red", "blue"}}}}
	if err := r.Register(spec); err != nil {
		t.Fatal(err)
	}
	spec.Outcomes[0] = "mutated"
	spec.Attributes["flavors"].Values[0] = "mutated"
	got, _ := r.Resolve("consumer")
	got.Outcomes[0] = "also-mutated"
	got.Attributes["flavors"].Values[0] = "also-mutated"
	g := &Graph{Start: "work", Nodes: []Node{{Name: "work", Action: "consumer", Outcomes: []string{"done", "wait"}, Attrs: map[string]string{"flavors": "red,blue"}}, {Name: "end", Terminal: true}}, Edges: []Edge{{From: "work", To: "end", On: "done"}}}
	opts := ValidationOptions{NoEdge: func(_ Node, outcome string) bool { return outcome == "wait" }}
	if err := Validate(g, r, opts); err != nil {
		t.Fatal(err)
	}
	if next, ok := Decide(g, "work", "done"); !ok || next != "end" {
		t.Fatalf("next %q %v", next, ok)
	}
	if _, ok := Decide(g, "work", "wait"); ok {
		t.Fatal("absent edge navigated")
	}
	g.Edges = append(g.Edges, g.Edges[0])
	if _, ok := Decide(g, "work", "done"); ok {
		t.Fatal("ambiguous edge navigated")
	}
	if err := Validate(g, r, opts); err == nil || !strings.Contains(err.Error(), "has 2 edges") {
		t.Fatalf("ambiguity: %v", err)
	}
	g.Nodes = append(g.Nodes, g.Nodes[0])
	if _, ok := g.Node("work"); ok {
		t.Fatal("duplicate node resolved")
	}
}

func TestParseRejectsUnsupportedDOT(t *testing.T) {
	for _, data := range []string{`graph { a -- b }`, `digraph { a:p -> b }`, `digraph { subgraph { a } }`, `digraph { a -> { b c } }`} {
		t.Run(data, func(t *testing.T) {
			if _, err := Parse([]byte(data)); err == nil {
				t.Fatal("accepted unsupported DOT")
			}
		})
	}
}

func TestBoundedCyclesAndClosedVocabulary(t *testing.T) {
	var r Registry
	if err := r.Register(ActionSpec{Name: "step", Outcomes: []string{"again", "done"}, Attributes: map[string]AttributeSpec{"kind": {Required: true, Values: []string{"allowed"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		limit int
		cycle bool
		kind  string
		want  []string
	}{
		{name: "bounded", limit: 2, cycle: true, kind: "allowed"},
		{name: "unbounded and unknown value", cycle: true, kind: "other", want: []string{"outside declared values", "cycle without positive limit"}},
		{name: "negative bound outside cycle", limit: -1, kind: "allowed", want: []string{"limit must be positive when supplied"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := Node{Name: "a", Action: "step", Outcomes: []string{"done"}, Limit: tc.limit, Attrs: map[string]string{"kind": tc.kind}}
			edges := []Edge{{From: "a", To: "end", On: "done"}}
			if tc.cycle {
				node.Outcomes = append(node.Outcomes, "again")
				edges = append(edges, Edge{From: "a", To: "a", On: "again"})
			}
			g := &Graph{Start: "a", Nodes: []Node{node, {Name: "end", Terminal: true}}, Edges: edges}
			err := Validate(g, r)
			if len(tc.want) == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected structural faults")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q in %v", want, err)
				}
			}
		})
	}
}

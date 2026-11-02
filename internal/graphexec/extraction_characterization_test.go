package graphexec_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestExtractionDOTUnsupportedForms(t *testing.T) {
	for _, body := range []string{`subgraph cluster { a; }`, `a:port -> b`, `a -> {b c}`, `a -> b [on=one,lf_on=two]`} {
		t.Run(body, func(t *testing.T) {
			_, err := graphexec.ParseProfile([]byte(`digraph {` + body + `}`))
			if err == nil || !strings.HasPrefix(err.Error(), "decode graph profile:") {
				t.Fatalf("want decode error, got %v", err)
			}
		})
	}
}

func TestExtractionDOTChainedEdgesKeepTemporalDefaults(t *testing.T) {
	p, err := graphexec.ParseProfile([]byte(`digraph {
 node [color=red]; a; node [color=blue]; b; c;
 edge [on=first,custom=one]; a -> b -> c;
 edge [on=second,custom=two]; c -> a [custom=override];
 }`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Nodes[0].Attrs["color"] != "red" || p.Nodes[1].Attrs["color"] != "blue" {
		t.Fatalf("node defaults: %+v", p.Nodes)
	}
	want := []graphexec.Edge{{From: "a", To: "b", On: "first", Attrs: map[string]string{"on": "first", "custom": "one"}}, {From: "b", To: "c", On: "first", Attrs: map[string]string{"on": "first", "custom": "one"}}, {From: "c", To: "a", On: "second", Attrs: map[string]string{"on": "second", "custom": "override"}}}
	if !reflect.DeepEqual(p.Edges, want) {
		t.Fatalf("edges: %+v", p.Edges)
	}
	p.Edges[0].Attrs["custom"] = "mutated"
	if p.Edges[1].Attrs["custom"] != "one" {
		t.Fatal("chained edges share attributes")
	}
}

func TestExtractionDecideRejectsAmbiguousAndTerminalEdges(t *testing.T) {
	for _, mode := range []string{"ambiguous", "empty target", "terminal", "undeclared outcome"} {
		t.Run(mode, func(t *testing.T) {
			p := profile()
			switch mode {
			case "ambiguous":
				p.Edges = append(p.Edges, p.Edges[0])
			case "empty target":
				p.Edges[0].To = ""
			case "terminal":
				p.Nodes[0].Terminal = true
			case "undeclared outcome":
				p.Nodes[0].Outcomes = nil
			}
			if target, ok := graphexec.Decide(p, "work", "done"); ok || target != "" {
				t.Fatalf("selected %q, %v", target, ok)
			}
		})
	}
}

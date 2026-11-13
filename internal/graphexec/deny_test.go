package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/denypolicy"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestDenySelectionValidation(t *testing.T) {
	p := profile()
	p.Attrs = map[string]string{"lf_deny": "local, local"}
	p.Nodes[0].Attrs["lf_deny"] = ""
	var r graphexec.Registry
	if err := r.Register(graphexec.ActionSpec{Agent: true, Name: "lf_implement_work", Mode: agent.ModeImplement, Outcomes: []string{"done", "again", "failed"}}); err != nil {
		t.Fatal(err)
	}
	if err := graphexec.Validate(p, r); err != nil {
		t.Fatalf("static validation needs no catalogue: %v", err)
	}
	if err := graphexec.ValidateDenySelections(p, nil); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("unknown group: %v", err)
	}
	if err := graphexec.ValidateDenySelections(p, denypolicy.Catalogue{"local": {Deny: []string{"Agent"}}}); err != nil {
		t.Fatal(err)
	}
	p.Nodes[1].Attrs = map[string]string{"lf_deny": ""}
	if err := graphexec.Validate(p, r); err == nil || !strings.Contains(err.Error(), "only valid on agent") {
		t.Fatalf("terminal empty selection: %v", err)
	}
	delete(p.Nodes[1].Attrs, "lf_deny")
	if err := graphexec.Validate(p, registry(t)); err == nil || !strings.Contains(err.Error(), "only valid on agent") {
		t.Fatalf("non-agent empty selection: %v", err)
	}
	p.Attrs["lf_deny"] = "Bash(make check)"
	if err := graphexec.Validate(p, r); err == nil || !strings.Contains(err.Error(), "graph lf_deny") {
		t.Fatalf("raw graph rule: %v", err)
	}
}

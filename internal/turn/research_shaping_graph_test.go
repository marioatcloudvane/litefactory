package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// The shipped graph is the two-stage one now, so these assert what actually
// runs rather than an aspiration. Ordering, per-stage scoping and both durable
// waits are the properties a second stage depends on.
func shapingProfile(t *testing.T) *graphexec.Profile {
	t.Helper()
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsource.CaptureResearch(t.Context(), specs)
	if err != nil {
		t.Fatalf("capture shipped Research: %v", err)
	}
	if err := graphexec.ValidateResearchControlGraph(definition.Profile); err != nil {
		t.Fatalf("shipped graph does not validate: %v", err)
	}
	return definition.Profile
}

// Clarification hands to shaping through a published report, never directly.
// The owner sees the findings before anyone decides what to build from them.
func TestShapingGraphPublishesBeforeShaping(t *testing.T) {
	p := shapingProfile(t)
	report, ok := graphexec.Decide(p, "loop", "false")
	if !ok || report != "report" {
		t.Fatalf("clarification does not end in a report: %q", report)
	}
	node, _ := p.Node(report)
	if node.Attrs["lf_kind"] != "final" || node.Attrs["lf_source"] != "investigate" {
		t.Fatalf("report is not investigate's final publication: %+v", node.Attrs)
	}
	if next, ok := graphexec.Decide(p, report, "published"); !ok || next != "shapeloop" {
		t.Fatalf("the report does not hand off to shaping: %q", next)
	}
}

// Each stage reads its own agent. Sharing a source is how shaping would inherit
// clarification's "done" and fall straight through without ever running.
func TestShapingGraphScopesEachStageToItsAgent(t *testing.T) {
	p := shapingProfile(t)
	for node, want := range map[string]string{
		"loop": "investigate", "question": "investigate", "ask": "investigate", "report": "investigate",
		"shapeloop": "shape", "shaped": "shape", "clarify": "shape", "agreed": "shape",
	} {
		n, ok := p.Node(node)
		if !ok {
			t.Errorf("no node %q", node)
			continue
		}
		if got := n.Attrs["lf_source"]; got != want {
			t.Errorf("node %q reads %q, want %q", node, got, want)
		}
	}
}

// Both stages rest at a durable wait when they ask, and only shaping's
// completion ends the workflow.
func TestShapingGraphEndsOnlyAfterShaping(t *testing.T) {
	p := shapingProfile(t)
	for _, name := range []string{"ask", "clarify", "agreed"} {
		next, ok := graphexec.Decide(p, name, "published")
		node, found := p.Node(next)
		if !ok || !found || !node.Terminal {
			t.Errorf("%q does not end at a terminal: %q", name, next)
		}
	}
}

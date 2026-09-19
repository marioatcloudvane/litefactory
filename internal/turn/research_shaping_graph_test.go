package turn

import (
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// shapingDOT is the two-stage graph shaping.md is written for. It is not the
// shipped graph: research_workflow.go refuses a second agent inside one run,
// and it has to, because one comment causes at most one run and a second agent
// needs a run of its own. Wiring the daemon's due-work admission path is what
// unblocks it. Until then this keeps the intended topology honest — the layout,
// the per-stage scoping and the publish-before-shape ordering are all validated
// here, so flipping the shipped graph over is a one-file change.
const shapingDOT = `digraph research {
 graph [team="builtin", mode="research", profile_version="1", start="loop"];
 loop [lf_action="lf_research_loop", lf_outcomes="true,false", lf_field="response", lf_condition="ask", lf_default="ask", lf_limit="8", lf_source="investigate"];
 investigate [lf_action="lf_research_agent", lf_outcomes="success,failed", lf_agent_markdown="research", lf_skills="research-evidence", lf_limit="8"];
 question [lf_action="lf_research_if", lf_outcomes="true,false", lf_field="response", lf_condition="ask", lf_limit="8", lf_source="investigate"];
 ask [lf_action="lf_research_comment", lf_outcomes="published", lf_kind="question", lf_limit="8", lf_source="investigate"];
 report [lf_action="lf_research_comment", lf_outcomes="published", lf_kind="final", lf_limit="1", lf_source="investigate"];
 shapeloop [lf_action="lf_research_loop", lf_outcomes="true,false", lf_field="response", lf_condition="ask", lf_default="ask", lf_limit="8", lf_source="shape"];
 shape [lf_action="lf_research_agent", lf_outcomes="success,failed", lf_agent_markdown="shaping", lf_skills="research-evidence", lf_limit="8"];
 shaped [lf_action="lf_research_if", lf_outcomes="true,false", lf_field="response", lf_condition="ask", lf_limit="8", lf_source="shape"];
 clarify [lf_action="lf_research_comment", lf_outcomes="published", lf_kind="question", lf_limit="8", lf_source="shape"];
 agreed [lf_action="lf_research_comment", lf_outcomes="published", lf_kind="final", lf_limit="1", lf_source="shape"];
 failed [lf_action="lf_research_comment", lf_outcomes="published", lf_kind="failure", lf_limit="1"];
 waiting [lf_terminal="true"];complete [lf_terminal="true"];stopped [lf_terminal="true"];
 loop -> investigate [lf_on="true"];loop -> report [lf_on="false"];
 investigate -> question [lf_on="success"];question -> ask [lf_on="true"];question -> loop [lf_on="false"];
 ask -> waiting [lf_on="published"];report -> shapeloop [lf_on="published"];
 shapeloop -> shape [lf_on="true"];shapeloop -> agreed [lf_on="false"];
 shape -> shaped [lf_on="success"];shaped -> clarify [lf_on="true"];shaped -> shapeloop [lf_on="false"];
 clarify -> waiting [lf_on="published"];agreed -> complete [lf_on="published"];
 investigate -> failed [lf_on="failed"];shape -> failed [lf_on="failed"];failed -> stopped [lf_on="published"];
}`

func shapingProfile(t *testing.T) *graphexec.Profile {
	t.Helper()
	p, err := graphexec.ParseProfile([]byte(shapingDOT))
	if err != nil {
		t.Fatalf("parse two-stage graph: %v", err)
	}
	if err := graphexec.ValidateResearchControlGraph(p); err != nil {
		t.Fatalf("two-stage graph does not validate: %v", err)
	}
	return p
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

// The role the second stage needs is in the embedded package already, so wiring
// the graph never waits on writing it. It is not captured while no node names
// it: capture reads what the graph references, and the shipped graph has one
// stage. This asserts the source is there and says what it must contain.
func TestShapingRoleIsReadyToWire(t *testing.T) {
	raw, err := os.ReadFile("../teamsource/research/shaping.md")
	if err != nil {
		t.Fatalf("the shaping role is missing from the embedded package: %v", err)
	}
	for _, want := range []string{"name: shaping", "# Role", "# Voice", "# Goal", "# How You Are Invoked", "# Must Do", "# Must Not Do", "# Output"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("shaping.md is missing %q", want)
		}
	}
	if !strings.Contains(string(raw), "audience") {
		t.Error("the stage that owns the questions must name their audience")
	}
	if node, ok := shapingProfile(t).Node("shape"); !ok || node.Attrs["lf_agent_markdown"] != "shaping" {
		t.Error("the two-stage graph does not select the shaping role")
	}
}

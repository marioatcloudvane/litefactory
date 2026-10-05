package turn

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

var fencedJSON = regexp.MustCompile("(?s)```json\\n(.*?)\\n```")

// The worked results moved out of the always-loaded instruction channel and
// into a skill resource, so nothing compiles them any more. This is what keeps
// them honest: every result the shipped resource teaches is exactly what the
// agent submits, is accepted by the shared validator, and routes to the
// response its section names under the factory's own derivation (#410).
func TestShippedNodeResultResourceDecodes(t *testing.T) {
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsource.CaptureResearch(t.Context(), specs)
	if err != nil {
		t.Fatal(err)
	}
	const path = "skills/research-evidence/node-result.md"
	raw, ok := definition.Files[path]
	if !ok {
		t.Fatalf("shipped Research package does not carry %s", path)
	}
	blocks := fencedJSON.FindAllSubmatch(raw, -1)
	if len(blocks) != 3 {
		t.Fatalf("want one worked result per derived response (ask, done, continue), got %d", len(blocks))
	}
	scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "research", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
	seen := map[string]bool{}
	for _, block := range blocks {
		if strings.Contains(string(block[1]), `"version"`) || strings.Contains(string(block[1]), `"response"`) {
			t.Errorf("shipped resource still teaches a printed envelope: %s", block[1])
		}
		result, d := agentresult.Create(block[1], scope)
		if !d.Valid {
			t.Errorf("shipped result is rejected by the shared validator: %+v", d.Diagnostics)
			continue
		}
		response, err := researchRetainedRoute(result)
		if err != nil {
			t.Errorf("shipped result does not route: %v", err)
			continue
		}
		seen[response] = true
	}
	for _, response := range []string{"ask", "done", "continue"} {
		if !seen[response] {
			t.Errorf("shipped resource teaches no result that routes to %q", response)
		}
	}
}

// What stays in the instruction channel has to be enough on its own to avoid
// the rejections an agent cannot recover from, has to say where the rest lives,
// and must never ask for a printed JSON result (#410).
func TestResearchNodeGuidanceKeepsTheFatalRulesInline(t *testing.T) {
	guidance := researchNodeGuidance()
	for _, want := range []string{
		researchNodeInstructions,
		"communication preflight",
		"communication submit",
		"communication receipt",
		"Do not print a JSON result",
		"terminal text is never read as a result",
		"nonblank result.reason",
		"outcome completed with no result.questions",
		"node-result",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("guidance is missing %q", want)
		}
	}
	for _, banned := range []string{"```json", `"version":1`, "Return exactly one JSON object", "Envelope:", "terminal node result only confirms"} {
		if strings.Contains(guidance, banned) {
			t.Errorf("guidance still carries %q", banned)
		}
	}
}

// Graph Plan guidance carries the same route and never asks for a printed
// result (#410).
func TestPlanNodeGuidanceUsesTheSharedRouteOnly(t *testing.T) {
	for _, want := range []string{"communication preflight", "communication submit", "communication receipt", "Do not print a JSON result", "terminal text is never read as a result", "storyrequest", "storyset"} {
		if !strings.Contains(planNodeGuidance, want) {
			t.Errorf("Plan guidance is missing %q", want)
		}
	}
	for _, banned := range []string{"return exactly one JSON object", `{"version":1`, "terminal object only confirms", "whose result is exactly"} {
		if strings.Contains(planNodeGuidance, banned) {
			t.Errorf("Plan guidance still carries %q", banned)
		}
	}
}

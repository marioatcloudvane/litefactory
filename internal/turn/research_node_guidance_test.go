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

// The worked envelopes moved out of the always-loaded instruction channel and
// into a skill resource, so nothing compiles them any more. This is what keeps
// them honest: every envelope the shipped resource teaches is decoded by the
// same function that would reject it at runtime.
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
	if len(blocks) != 2 {
		t.Fatalf("want one worked envelope per response, got %d", len(blocks))
	}
	scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "research", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
	seen := map[string]bool{}
	for _, block := range blocks {
		node, d := agentresult.DecodeResearchNode(block[1], scope)
		if !d.Valid {
			t.Errorf("shipped envelope is rejected by DecodeResearchNode: %+v", d.Diagnostics)
			continue
		}
		seen[node.Response] = true
	}
	for _, response := range []string{"ask", "done"} {
		if !seen[response] {
			t.Errorf("shipped resource teaches no %q envelope", response)
		}
	}
}

// What stays in the instruction channel has to be enough on its own to avoid
// the two rejections an agent cannot recover from, and has to say where the
// rest lives.
func TestResearchNodeGuidanceKeepsTheFatalRulesInline(t *testing.T) {
	guidance := researchNodeGuidance()
	for _, want := range []string{
		researchNodeInstructions,
		`"version":1`,
		"nonblank result.reason",
		"outcome completed and no result.questions",
		"node-result",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("guidance is missing %q", want)
		}
	}
	if strings.Contains(guidance, "```json") {
		t.Error("worked envelopes belong in the on-demand skill resource, not the always-loaded channel")
	}
}

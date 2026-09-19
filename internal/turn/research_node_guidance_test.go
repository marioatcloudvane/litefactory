package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// The delivered examples are the node contract the agent actually reads. If
// they stop decoding, the factory is teaching a shape it would then reject.
func TestResearchNodeExamplesSatisfyTheDecoder(t *testing.T) {
	ask, done, err := researchNodeExamples()
	if err != nil {
		t.Fatalf("render node examples: %v", err)
	}
	ctx := agentresult.Context{SchemaVersion: 1, Mode: "research", WorkflowVersion: 2, QuestionsAllowed: true}
	for name, data := range map[string][]byte{"ask": ask, "done": done} {
		node, d := agentresult.DecodeResearchNode(data, ctx)
		if !d.Valid {
			t.Errorf("%s example is rejected by DecodeResearchNode: %+v", name, d.Diagnostics)
			continue
		}
		if node.Response != name {
			t.Errorf("%s example decoded as response %q", name, node.Response)
		}
	}
}

func TestResearchNodeGuidanceCarriesBothEnvelopes(t *testing.T) {
	guidance, err := researchNodeGuidance()
	if err != nil {
		t.Fatalf("render node guidance: %v", err)
	}
	if !strings.Contains(guidance, researchNodeInstructions) {
		t.Error("guidance dropped the prose obligations")
	}
	for _, want := range []string{`"response": "ask"`, `"response": "done"`, "```json"} {
		if !strings.Contains(guidance, want) {
			t.Errorf("guidance is missing %q", want)
		}
	}
}

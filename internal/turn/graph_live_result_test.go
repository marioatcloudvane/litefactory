package turn

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

// submitLiveResult retains an envelope's result through the shared result route,
// as a real agent does before it answers. The terminal envelope alone never
// supplies a result, so a fake host that returns one must submit it first.
func submitLiveResult(t *testing.T, req agent.Request, envelope string) {
	t.Helper()
	var node struct {
		Result json.RawMessage `json:"result"`
	}
	mustProgress(t, json.Unmarshal([]byte(envelope), &node))
	data := append(append([]byte(`{"result":`), node.Result...), '}')
	got := mcpDecodeSubmission(t, mcpToolCall(t, submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, "communication_submit", data))
	if !got.Validation.Valid || got.Receipt == nil {
		t.Fatalf("live result refused: %+v", got.Validation)
	}
}

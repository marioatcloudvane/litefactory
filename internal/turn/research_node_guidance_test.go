package turn

import (
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"regexp"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
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
	definition, err := teamsourcetest.CaptureResearch(t.Context(), specs)
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

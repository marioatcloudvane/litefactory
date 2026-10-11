package agentresult

import (
	"strings"
	"testing"
)

// A coordinator agent (the Implement sync resolver) works on the feature branch,
// not on a story: its completed result needs no story scope and names no story.
func TestImplementCoordinatorRoleNeedsAndNamesNoStory(t *testing.T) {
	completed := Result{Outcome: "completed", Comment: "Resolved the conflict.", Payload: Payload{Implement: &Implement{}}}
	coordinator := Context{SchemaVersion: SchemaVersion, Mode: "implement", WorkflowVersion: 2, CoordinatorRole: true}
	if d := ValidateResult(completed, coordinator); !d.Valid {
		t.Fatalf("a completed coordinator result without stories: %+v", d)
	}
	story := coordinator
	story.CoordinatorRole = false
	if d := ValidateResult(completed, story); d.Valid {
		t.Fatal("a story worker's completed result still needs its story scope")
	}
	named := completed
	named.Payload = Payload{Implement: &Implement{Stories: []ImplementationStory{{Story: "story-001", Outcome: "engineering_complete"}}}}
	d := ValidateResult(named, coordinator)
	if d.Valid {
		t.Fatal("a coordinator result must name no story")
	}
	found := false
	for _, e := range d.Diagnostics {
		found = found || strings.Contains(e.Expected, "no stories from a coordinator agent")
	}
	if !found {
		t.Fatalf("diagnostics do not explain the refusal: %+v", d)
	}
}

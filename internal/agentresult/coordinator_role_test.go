package agentresult

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
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

// #1142: of the Implement agents only the sync resolver may ask, and only on an
// unfinished result; a story worker's result with questions is still refused.
func TestImplementOnlyTheCoordinatorRoleMayAsk(t *testing.T) {
	ask := []Question{{ID: "q1", Audience: "technical", Text: "Which handler should stay?", Options: []Option{{ID: "a", Text: "Keep the branch's"}, {ID: "b", Text: "Take main's"}}, RecommendedOption: "b"}}
	coordinator := Context{SchemaVersion: SchemaVersion, Mode: "implement", WorkflowVersion: 2, CoordinatorRole: true, QuestionsAllowed: true}
	for _, outcome := range []string{"incomplete", "blocked"} {
		r := Result{Outcome: outcome, Comment: "Two handlers conflict.", Reason: "main and the stories disagree.", Questions: ask, Payload: Payload{Implement: &Implement{}}}
		if d := ValidateResult(r, coordinator); !d.Valid {
			t.Fatalf("a %s resolver result may ask: %+v", outcome, d)
		}
	}
	completed := Result{Outcome: "completed", Comment: "Resolved.", Questions: ask, Payload: Payload{Implement: &Implement{}}}
	if d := ValidateResult(completed, coordinator); d.Valid {
		t.Fatal("a completed resolver result must not ask")
	}
	worker := Context{SchemaVersion: SchemaVersion, Mode: "implement", WorkflowVersion: 2, QuestionsAllowed: true, Stories: map[string]agent.ContributionStory{"story-001": {Revision: "r1"}}}
	blocked := Result{Outcome: "blocked", Comment: "Stuck.", Reason: "need a key.", Questions: ask, Payload: Payload{Implement: &Implement{}}}
	if d := ValidateResult(blocked, worker); d.Valid {
		t.Fatal("a story worker's result with questions is refused")
	}
	blocked.Questions = nil
	if d := ValidateResult(blocked, worker); !d.Valid {
		t.Fatalf("the story worker's blocked result without questions: %+v", d)
	}
}

func TestReturnAllocationIsCoordinatorAdviceOnly(t *testing.T) {
	raw := []byte(`{"result":{"outcome":"completed","comment":"Resolved the base conflict.","payload":{"implement":{"return_allocation":[{"story":"story-001"}]}}}}`)
	coordinator := Context{SchemaVersion: SchemaVersion, Mode: "implement", WorkflowVersion: 2, CoordinatorRole: true}
	result, d := Create(raw, coordinator)
	if !d.Valid || len(result.Payload.Implement.ReturnAllocation) != 1 {
		t.Fatalf("coordinator allocation: %+v %+v", result, d)
	}
	worker := coordinator
	worker.CoordinatorRole = false
	if _, d := Create(raw, worker); d.Valid {
		t.Fatal("worker may not allocate returned work")
	}
}

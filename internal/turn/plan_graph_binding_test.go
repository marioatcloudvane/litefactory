package turn

import (
	"context"
	"slices"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

const graphBindingDraft = `{"key":"story-001","name":"Add export","user_story":{"actor":"owner","want":"export data","benefit":"share it","notes":[]},"problem":"No export exists.","in_scope":["export"],"not_in_scope":[],"dependencies":{"story_keys":[],"issue_refs":[]},"frontend":false}`

func TestPlanGraphBinding_FactoryValuesComeFromState(t *testing.T) {
	state := planbrief.PlanState{SharedRevision: "shared-7"}
	binding, err := planGraphBinding(state, "story-001", "story", 4, 9, "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Story != "story-001" || binding.Stage != "story" || binding.Candidate != "plan-4-9" || binding.RequestID != "plan-request-4-9" || binding.SharedRevision != "shared-7" || binding.ExistingRevision != "" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	next, err := planGraphBinding(state, "story-001", "story", 4, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.Candidate == binding.Candidate || next.RequestID == binding.RequestID {
		t.Fatal("distinct attempts share request identity")
	}
	if !slices.Contains(planGraphResultTargets(), ar.Target{Field: "payload.plan.storyrequest"}) || !slices.Contains(planGraphResultTargets(), ar.Target{Field: "payload.plan.summary"}) {
		t.Fatal("graph Plan grants are not the legacy targets plus the branches")
	}
}

// One resolver serves preflight and submit, so both run the same validation, and
// a state change between them refuses bytes preflight accepted without retaining
// them. Preflight itself allocates nothing.
func TestAgentResultService_GraphPlanPreflightAndSubmitShareValidation(t *testing.T) {
	db, _, o := agentResultFixture(t, "plan")
	binding := &ar.PlanGraphBinding{Story: "story-001", Stage: "story", Candidate: "plan-1-1", RequestID: "plan-request-1-1", Upstream: []string{}, SharedRevision: "initial"}
	o.Resolve = func(context.Context) (ar.Context, error) {
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}, PlanGraph: binding.Clone()}, nil
	}
	o.ReadTargets, o.EditTargets = planGraphResultTargets(), planGraphResultTargets()
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"result":{"outcome":"completed","comment":"Proposed.","payload":{"plan":{"storyrequest":{"operation":"create","draft":` + graphBindingDraft + `}}}}}`)
	restricted := context.WithValue(t.Context(), mcpCommunicationKey{}, true)
	first, err := service.Preflight(restricted, data)
	if err != nil || !first.Valid {
		t.Fatalf("preflight refused a graph Plan story request: %+v %v", first, err)
	}
	if revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatal("preflight retained a revision")
	}
	again, err := service.Preflight(restricted, data)
	if err != nil || again.Valid != first.Valid || len(again.Diagnostics.Diagnostics) != len(first.Diagnostics.Diagnostics) {
		t.Fatal("identical bytes changed outcome with unchanged state")
	}
	// The story acquired an issue draft after preflight: the same bytes are stale.
	binding.ExistingRevision = strings.Repeat("c", 64)
	refused, err := service.Submit(restricted, "req-1", data)
	if err != nil {
		t.Fatal(err)
	}
	if refused.Validation.Valid || refused.Snapshot != nil {
		t.Fatalf("stale content accepted at submit: %+v", refused.Validation)
	}
	if revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatal("stale content retained")
	}
	binding.ExistingRevision = ""
	accepted, err := service.Submit(restricted, "req-2", data)
	if err != nil || !accepted.Validation.Valid || accepted.Snapshot == nil {
		t.Fatalf("submit refused: %+v %v", accepted.Validation, err)
	}
	q := accepted.Snapshot.Result.Payload.Plan.StoryRequest
	if q.Stage != "story" || q.Candidate != "plan-1-1" || q.RequestID != "plan-request-1-1" || q.SharedRevision != "initial" {
		t.Fatalf("recorded values are not the factory's: %+v", q)
	}
	supplied := []byte(strings.Replace(string(data), `"operation":"create",`, `"operation":"create","stage":"technical",`, 1))
	if d, err := service.Preflight(restricted, supplied); err != nil || d.Valid {
		t.Fatalf("supplied factory field accepted at preflight: %+v %v", d, err)
	}
}

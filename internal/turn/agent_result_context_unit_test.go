package turn

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestCloneResultContext_NestedScopeIndependence(t *testing.T) {
	t.Parallel()
	original := agentresult.Context{RequireAggregate: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}, IssueRefs: []string{"issue-1"}}, EngineeringComplete: map[string]bool{"US-001": true}, Stories: map[string]agent.ContributionStory{"US-001": {Criteria: []string{"AC-001"}, ExecutionRequired: []string{"AC-001"}, Checks: map[string]agent.RetainedContributionCheck{"check-1": {Passed: true, Retained: true}}}}}
	before, err := json.Marshal(original.Stories)
	if err != nil {
		t.Fatal(err)
	}
	copied := cloneResultContext(original)
	copied.PlanScope.Roles[0] = "changed"
	copied.PlanScope.IssueRefs[0] = "changed"
	copied.EngineeringComplete["US-001"] = false
	story := copied.Stories["US-001"]
	story.Criteria[0] = "AC-changed"
	story.ExecutionRequired[0] = "AC-changed"
	story.Checks["check-1"] = agent.RetainedContributionCheck{}
	copied.Stories["US-002"] = story
	after, err := json.Marshal(original.Stories)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || original.PlanScope.Roles[0] != "engineer" || original.PlanScope.IssueRefs[0] != "issue-1" || !original.EngineeringComplete["US-001"] || !copied.RequireAggregate {
		t.Fatal("copied scope aliases retained authority or relaxes aggregate requirement")
	}
}

func TestAggregateResultInput_RejectsNonAuthoredShape(t *testing.T) {
	t.Parallel()
	valid := `{"engineering":{"snapshot_id":"aggregate-1","check_ids":["check-1"]},"capture":{"implementation_digest":"digest","files":[],"checks":[]}}`
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"typed authored fields", valid, true},
		{"factory run", strings.Replace(valid, `{"engineering"`, `{"run_id":1,"engineering"`, 1), false},
		{"duplicate snapshot", strings.Replace(valid, `"snapshot_id":"aggregate-1"`, `"snapshot_id":"aggregate-1","snapshot_id":"other"`, 1), false},
		{"unknown capture field", strings.Replace(valid, `"files":[]`, `"files":[],"accepted":true`, 1), false},
		{"wrong check ids", strings.Replace(valid, `["check-1"]`, `"check-1"`, 1), false},
		{"trailing object", valid + `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var input aggregateResultInput
			err := decodeMCPArguments([]byte(tc.input), &input)
			if (err == nil) != tc.valid {
				t.Fatalf("shape valid=%t, want=%t, err=%v", err == nil, tc.valid, err)
			}
			if tc.valid && (input.Engineering.SnapshotID != "aggregate-1" || !reflect.DeepEqual(input.Engineering.CheckIDs, []string{"check-1"})) {
				t.Fatalf("typed references changed: %+v", input)
			}
		})
	}
}

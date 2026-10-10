package turn

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// Only an explicit (Manual) marker in the frozen criterion text exempts a
// criterion from executed proof; unmarked and (Auto) criteria stay required.
func TestStoryCriteriaExecutionRequiredFollowsFrozenMarker(t *testing.T) {
	p, _, _, _ := typedPlanFixture(t)
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, db.Close()) })
	mustProgress(t, db.Migrate(t.Context()))
	parent := "github:o/r#1"
	mustProgress(t, db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning"}))
	mustProgress(t, db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: parent, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	story := p.Stories[0]
	story.AcceptanceCriteria = []planbrief.CriterionContent{{ID: "AC-001", Text: "Health answers ok"}, {ID: "AC-002", Text: "(Manual) The log line names the request"}, {ID: "AC-003", Text: "(Auto) The end-to-end health check passes"}}
	for i := range story.Tasks {
		story.Tasks[i].CriterionIDs = []string{"AC-001", "AC-002", "AC-003"}
	}
	body := "## Acceptance Criteria\n- AC-001: Health answers ok\n- AC-002: (Manual) The log line names the request\n- AC-003: (Auto) The end-to-end health check passes\n"
	revision := planbrief.StoryHash(story.Title, body)
	raw, err := json.Marshal(story)
	mustProgress(t, err)
	mustProgress(t, db.PutSubmissionProjection(t.Context(), store.SubmissionProjection{Parent: parent, Generation: 1, Story: "US-001", Revision: revision, Title: story.Title, Body: body, ContentVersion: 2, IssueID: 123, Content: raw, Criteria: []string{"AC-001", "AC-002", "AC-003"}}))
	ids, required, err := storyCriteria(t.Context(), db, parent, 1, "US-001", revision, body)
	mustProgress(t, err)
	if !reflect.DeepEqual(ids, []string{"AC-001", "AC-002", "AC-003"}) || !reflect.DeepEqual(required, []string{"AC-001", "AC-003"}) {
		t.Fatalf("typed: ids %v required %v", ids, required)
	}

	legacyBody := "## Acceptance Criteria\n- legacy exact criterion\n- AC-002: (Manual) reviewed by reading\n- (Auto) end-to-end check passes\n- (Manual) (Auto) both markers fail closed\n"
	ids, required, err = storyCriteria(t.Context(), db, "github:o/r#99", 1, "US-001", revision, legacyBody)
	mustProgress(t, err)
	if !reflect.DeepEqual(ids, []string{"legacy exact criterion", "AC-002", "(Auto) end-to-end check passes", "(Manual) (Auto) both markers fail closed"}) {
		t.Fatalf("legacy names changed: %v", ids)
	}
	if !reflect.DeepEqual(required, []string{"legacy exact criterion", "(Auto) end-to-end check passes", "(Manual) (Auto) both markers fail closed"}) {
		t.Fatalf("legacy required: %v", required)
	}
}

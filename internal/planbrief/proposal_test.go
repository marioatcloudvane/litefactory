package planbrief

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanProposalRejectsIncompleteAndAmbiguousContracts(t *testing.T) {
	body := "## User Story\nAs owner.\n## Acceptance Criteria\nWorks.\n## Tasks\nBuild.\n## Architecture\nUse store.\n## Dependencies\nNone.\n"
	p := PlanProposal{Version: 1, Parent: "github:o/r#1", Generation: 1, Stories: []StoryProposal{{Key: "US-1", Intent: "create", Title: "Complete", Body: body}}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProposal(data); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"User Story", "Acceptance Criteria", "Tasks", "Architecture", "Dependencies"} {
		t.Run(section, func(t *testing.T) {
			q := p
			q.Stories = append([]StoryProposal(nil), p.Stories...)
			q.Stories[0].Body = strings.Replace(body, "## "+section, "## Removed", 1)
			if err := q.Validate(); err == nil {
				t.Fatal("missing required section accepted")
			}
		})
	}
	t.Run("cycle", func(t *testing.T) {
		q := p
		q.Stories = append([]StoryProposal(nil), p.Stories...)
		q.Stories[0].Dependencies = []string{"US-2"}
		q.Stories = append(q.Stories, StoryProposal{Key: "US-2", Intent: "create", Title: "Second", Body: body, Dependencies: []string{"US-1"}})
		if err := q.Validate(); err == nil {
			t.Fatal("cycle accepted")
		}
	})
	for _, tc := range []struct {
		name   string
		mutate func(*PlanProposal)
	}{
		{"unknown dependency", func(q *PlanProposal) { q.Stories[0].Dependencies = []string{"US-missing"} }},
		{"duplicate story key", func(q *PlanProposal) { q.Stories = append(q.Stories, q.Stories[0]) }},
		{"duplicate issue identity", func(q *PlanProposal) {
			q.Stories[0].Intent = "update"
			q.Stories[0].IssueID, q.Stories[0].IssueNumber = 200, 20
			q.Stories[0].ExpectedBase = StoryHash("Complete", body)
			second := q.Stories[0]
			second.Key = "US-2"
			q.Stories = append(q.Stories, second)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := p
			q.Stories = append([]StoryProposal(nil), p.Stories...)
			tc.mutate(&q)

			if err := q.Validate(); err == nil {
				t.Fatal("ambiguous or unresolved story scope accepted")
			}
		})
	}
	t.Run("unknown field", func(t *testing.T) {
		data = append([]byte(`{"unexpected":true,`), data[1:]...)
		if _, err := ParseProposal(data); err == nil {
			t.Fatal("unknown field accepted")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		if err := ValidateStoryBody(body + strings.Repeat("x", MaxStoryBodyBytes)); err == nil {
			t.Fatal("oversize accepted")
		}
	})
}

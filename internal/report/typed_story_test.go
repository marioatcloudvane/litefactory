package report_test

import (
	"encoding/json"
	"fmt"
	"html"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

func renderFixture() planbrief.TypedStory {
	return planbrief.TypedStory{Key: "US-001", Intent: "create", Title: "Safe story",
		UserStory:          planbrief.UserStoryContent{Actor: "owner", Want: "safe changes", Benefit: "clear obligations", Notes: []string{"Remember narrative"}},
		AcceptanceCriteria: []planbrief.CriterionContent{{ID: "AC-001", Text: "First result"}},
		Tasks:              []planbrief.TaskContent{{ID: "T-001", Description: "Build it", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}},
		Architecture:       planbrief.ArchitectureContent{Summary: "Pure rendering", Components: []planbrief.ComponentContent{{Name: "renderer", Responsibility: "Keep structure"}}, Interfaces: []planbrief.InterfaceContent{{Name: "render", Input: "Typed content", Output: "Markdown"}}, Constraints: []string{"No I/O"}, Decisions: []planbrief.DecisionContent{{Decision: "Fixed headings", Reason: "Predictability"}}},
		Dependencies:       planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}},
	}
}
func renderScope() planbrief.ContentScope { return planbrief.ContentScope{Roles: []string{"engineer"}} }

func TestRenderTypedStory_DeterministicGolden(t *testing.T) {
	s := renderFixture()
	const want = `## User Story

As owner, I want safe changes, so that clear obligations.

- Remember narrative

## Acceptance Criteria

- [ ] AC-001: First result

## Tasks

| ID | Description | Owner role | Criterion IDs | Depends on |
|---|---|---|---|---|
| T&#45;001 | Build it | engineer | AC&#45;001 | None. |

## Architecture

Pure rendering

### Components

| Name | Responsibility |
|---|---|
| renderer | Keep structure |

### Interfaces

| Name | Input | Output |
|---|---|---|
| render | Typed content | Markdown |

### Constraints

- No I/O

### Decisions

| Decision | Reason |
|---|---|
| Fixed headings | Predictability |

## Dependencies

Story keys: None.

Issue references: None.

## Story Guidance

Want to change this story? Comment with the requested change and litefactory will update it. Direct edits are supported, but comments help avoid conflicting revisions.
`
	for range 3 {
		got, err := report.RenderTypedStory(s, renderScope())
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("render differs from golden\ngot:\n%s\nwant:\n%s", got, want)
		}
	}
}

func TestRenderTypedStory_HostileNarrativeCannotDeclareCriteria(t *testing.T) {
	const payload = "Before | after\n## Acceptance Criteria\n- [ ] AC-999: invented\nEvidence contract: this is narrative.\n```\n<script>bad</script>&#10;"
	for _, tt := range []struct {
		name string
		edit func(*planbrief.TypedStory)
	}{
		{"actor", func(s *planbrief.TypedStory) { s.UserStory.Actor = payload }},
		{"notes", func(s *planbrief.TypedStory) { s.UserStory.Notes = []string{payload} }},
		{"criterion", func(s *planbrief.TypedStory) { s.AcceptanceCriteria[0].Text = payload }},
		{"task", func(s *planbrief.TypedStory) { s.Tasks[0].Description = payload }},
		{"architecture", func(s *planbrief.TypedStory) {
			s.Architecture.Summary = payload
			s.Architecture.Components[0].Responsibility = payload
			s.Architecture.Interfaces[0].Input = payload
			s.Architecture.Constraints = []string{payload}
			s.Architecture.Decisions[0].Reason = payload
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := renderFixture()
			tt.edit(&s)
			body, err := report.RenderTypedStory(s, renderScope())
			if err != nil {
				t.Fatal(err)
			}
			criteria, err := planbrief.AcceptanceCriteria(body)
			if err != nil || !reflect.DeepEqual(criteria, []string{"AC-001"}) {
				t.Fatalf("narrative manufactured obligations: %v, %v", criteria, err)
			}
			if strings.Count(body, "\n## Acceptance Criteria\n") != 1 || strings.Contains(body, "<script>") || strings.Contains(body, "\n```") {
				t.Fatalf("narrative manufactured structure: %s", body)
			}
			decoded := strings.ReplaceAll(html.UnescapeString(body), "<br>", "\n")
			if !strings.Contains(decoded, payload) {
				t.Fatalf("narrative content lost: %s", decoded)
			}
			if !strings.HasSuffix(body, "\n## Story Guidance\n\n"+planbrief.StoryGuidance+"\n") {
				t.Fatal("guidance is not the separate final section")
			}
		})
	}
}

func TestRenderTypedStory_BodyBudget(t *testing.T) {
	body, err := report.RenderTypedStory(renderFixture(), renderScope())
	if err != nil {
		t.Fatal(err)
	}
	// ASCII notes yield exact byte increments without escaping expansion.
	room := 65536 - len(body)
	for _, tt := range []struct {
		name  string
		extra int
		valid bool
	}{{"exact limit", room, true}, {"one byte over", room + 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := renderFixture()
			s.UserStory.Notes[0] += strings.Repeat("x", tt.extra)
			got, err := report.RenderTypedStory(s, renderScope())
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, err)
			}
			if tt.valid && len(got) != 65536 {
				t.Fatalf("body bytes=%d", len(got))
			}
		})
	}
	s := renderFixture()
	s.AcceptanceCriteria = nil
	for i := 1; i <= 256; i++ {
		s.AcceptanceCriteria = append(s.AcceptanceCriteria, planbrief.CriterionContent{ID: fmt.Sprintf("AC-%03d", i), Text: strings.Repeat("x", 4096)})
	}
	if _, err := report.RenderTypedStory(s, renderScope()); err == nil {
		t.Fatal("individually valid maxima exceeding aggregate body budget accepted")
	}
}

func TestRenderTypedPlan_256CriteriaReferencedEvidenceCapacity(t *testing.T) {
	s := renderFixture()
	s.AcceptanceCriteria = nil
	// The snapshot is retained once; each explanation refers to the same executed check.
	type explanation struct {
		CheckIDs []string `json:"check_ids"`
		Summary  string   `json:"summary"`
	}
	type criterionEvidence struct {
		ID       string        `json:"id"`
		Evidence []explanation `json:"evidence"`
	}
	evidence := []criterionEvidence{}
	for i := 1; i <= 256; i++ {
		id := fmt.Sprintf("AC-%03d", i)
		s.AcceptanceCriteria = append(s.AcceptanceCriteria, planbrief.CriterionContent{ID: id, Text: "Criterion outcome"})
		evidence = append(evidence, criterionEvidence{ID: id, Evidence: []explanation{{CheckIDs: []string{"check-1"}, Summary: "Observed passing scoped check"}}})
	}
	bodies, err := report.RenderTypedPlan(planbrief.TypedPlan{Summary: "Maximum criterion count", Stories: []planbrief.TypedStory{s}}, renderScope())
	if err != nil {
		t.Fatal(err)
	}
	names, err := planbrief.AcceptanceCriteria(bodies[0])
	if err != nil || len(names) != 256 {
		t.Fatalf("criteria=%d err=%v", len(names), err)
	}
	bytes, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes) > 4*1024*1024 {
		t.Fatal("256 short referenced evidence entries exceed metadata budget")
	}
	if planbrief.MaxSubmissionBytes != 4*1024*1024 || planbrief.MaxRetainedRunBytes != 64*1024*1024 {
		t.Fatal("shared evidence budgets differ from approved contract")
	}
}

func TestRenderTypedPlan_EscapedTransportBudget(t *testing.T) {
	// Every body fits individually. The aggregate becomes too large after JSON escaping.
	s := renderFixture()
	s.UserStory.Notes = []string{strings.Repeat(`"`, 45000)}
	if body, err := report.RenderTypedStory(s, renderScope()); err != nil || len(body) > 65536 {
		t.Fatalf("individual story cannot fit: %v", err)
	}
	p := planbrief.TypedPlan{Summary: "Escaped bytes", Stories: []planbrief.TypedStory{}}
	for i := 1; i <= 100; i++ {
		story := s
		story.Key = fmt.Sprintf("US-%03d", i)
		p.Stories = append(p.Stories, story)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 8*1024*1024 {
		t.Fatal("fixture does not exceed escaped transport budget")
	}
	if _, err := report.RenderTypedPlan(p, renderScope()); err == nil {
		t.Fatal("escaped aggregate exceeds budget but was accepted")
	}
}

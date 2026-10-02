package planbrief_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func draftFixture(stage string) planbrief.PlanDraft {
	d := planbrief.PlanDraft{Key: "story-a", Name: "Shape a story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "shared understanding"}, Problem: "Unclear requirements", InScope: []string{"Story conversation"}, NotInScope: []string{"Implementation"}}
	if stage != "story" {
		d.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Ownership", Decision: "Separate roles", Rationale: "Focused responsibilities"}}}
		d.Guidance = []string{"agent-setup"}
	}
	if stage == "acceptance" {
		d.Criteria = []planbrief.PlanCriterion{{ID: "AC-001", Text: "Draft retains intent", ProofMethod: "Unit test"}}
		d.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Implement draft", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}}}
	}
	return d
}

func TestPlanDraft_StageContracts(t *testing.T) {
	cases := []struct {
		name, stage string
		change      func(*planbrief.PlanDraft)
		invalid     bool
	}{
		{"usable initial draft/story", "story", nil, false},
		{"neutral architecture/technical", "technical", nil, false},
		{"complete links/acceptance", "acceptance", nil, false},
		{"prefixed agent title/reject", "story", func(d *planbrief.PlanDraft) { d.Name = "362-001: Story" }, true},
		{"missing problem/reject", "story", func(d *planbrief.PlanDraft) { d.Problem = "" }, true},
		{"early architecture/reject", "story", func(d *planbrief.PlanDraft) { d.Architecture = &planbrief.PlanArchitecture{} }, true},
		{"early criteria/reject", "technical", func(d *planbrief.PlanDraft) { d.Criteria = []planbrief.PlanCriterion{{ID: "AC-001"}} }, true},
		{"missing general/reject", "technical", func(d *planbrief.PlanDraft) { d.Architecture.General = nil }, true},
		{"nonfrontend design/reject", "technical", func(d *planbrief.PlanDraft) { d.Design = []string{"https://example.com/design"} }, true},
		{"frontend references/accept", "technical", func(d *planbrief.PlanDraft) { d.Frontend = true; d.Design = []string{"https://example.com/design"} }, false},
		{"missing proof/reject", "acceptance", func(d *planbrief.PlanDraft) { d.Criteria[0].ProofMethod = "" }, true},
		{"unknown criterion/reject", "acceptance", func(d *planbrief.PlanDraft) { d.Tasks[0].CriterionIDs = []string{"AC-002"} }, true},
		{"uncovered criterion/reject", "acceptance", func(d *planbrief.PlanDraft) {
			d.Criteria = append(d.Criteria, planbrief.PlanCriterion{ID: "AC-002", Text: "Second obligation", ProofMethod: "Unit test"})
		}, true},
		{"duplicate criterion/reject", "acceptance", func(d *planbrief.PlanDraft) { d.Criteria = append(d.Criteria, d.Criteria[0]) }, true},
		{"foreign owner/reject", "acceptance", func(d *planbrief.PlanDraft) { d.Tasks[0].OwnerRole = "unknown" }, true},
		{"cyclic task/reject", "acceptance", func(d *planbrief.PlanDraft) { d.Tasks[0].DependsOn = []string{"T-001"} }, true},
		{"foreign issue/reject", "story", func(d *planbrief.PlanDraft) { d.Dependencies.IssueRefs = []string{"github:foreign/repo#1"} }, true},
		{"duplicate story dependency/reject", "story", func(d *planbrief.PlanDraft) { d.Dependencies.StoryKeys = []string{"story-b", "story-b"} }, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := draftFixture(tt.stage)
			if tt.change != nil {
				tt.change(&d)
			}
			err := d.Validate(tt.stage, planbrief.ContentScope{Roles: []string{"engineer"}})
			if (err != nil) != tt.invalid {
				t.Fatalf("invalid=%v, got %v", tt.invalid, err)
			}
		})
	}
}

func TestMergePlanDraft_ScopedReuse(t *testing.T) {
	prior := draftFixture("technical")
	prior.Frontend = true
	prior.Design = []string{"https://example.com/design"}
	prior.Decisions = []string{"Keep focused roles"}
	prior.Assumptions = []string{"Owner is available"}
	next := planbrief.PlanDraft{Key: prior.Key, Questions: []string{"Which actor?"}, Decisions: prior.Decisions, Assumptions: prior.Assumptions}
	got, err := planbrief.MergePlanDraft(prior, next, []string{"conversation"})
	if err != nil {
		t.Fatal(err)
	}
	want := prior
	want.Questions = next.Questions
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unchanged sections lost: got %#v want %#v", got, want)
	}
	again, err := planbrief.MergePlanDraft(got, planbrief.PlanDraft{Key: prior.Key}, nil)
	if err != nil || !reflect.DeepEqual(again, got) {
		t.Fatalf("repeated cycle changed content: %#v %v", again, err)
	}
	removed, err := planbrief.MergePlanDraft(got, planbrief.PlanDraft{Key: prior.Key}, []string{"design"})
	if err != nil {
		t.Fatal(err)
	}
	want.Design = nil
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("explicit removal altered unrelated content: %#v", removed)
	}
	for _, tt := range []struct {
		name, key string
		sections  []string
	}{{"identity", "foreign", nil}, {"unknown section", prior.Key, []string{"arbitrary"}}, {"duplicate section", prior.Key, []string{"design", "design"}}} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := planbrief.MergePlanDraft(prior, planbrief.PlanDraft{Key: tt.key}, tt.sections); err == nil {
				t.Fatal("unsafe merge accepted")
			}
		})
	}
}

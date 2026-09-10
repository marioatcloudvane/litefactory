package planbrief_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func typedFixture() planbrief.TypedPlan {
	return planbrief.TypedPlan{Summary: "Plan summary", Stories: []planbrief.TypedStory{{
		Key: "US-001", Intent: "create", Title: "Typed story",
		UserStory:          planbrief.UserStoryContent{Actor: "owner", Want: "safe changes", Benefit: "clear requirements", Notes: []string{"A note"}},
		AcceptanceCriteria: []planbrief.CriterionContent{{ID: "AC-001", Text: "A result"}},
		Tasks:              []planbrief.TaskContent{{ID: "T-001", Description: "Implement", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}},
		Architecture:       planbrief.ArchitectureContent{Summary: "Pure functions", Components: []planbrief.ComponentContent{{Name: "parser", Responsibility: "validate"}}, Interfaces: []planbrief.InterfaceContent{{Name: "parse", Input: "JSON", Output: "content"}}, Constraints: []string{"No I/O"}, Decisions: []planbrief.DecisionContent{{Decision: "Typed fields", Reason: "Explicit contract"}}},
		Dependencies:       planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}},
	}}}
}
func typedScope() planbrief.ContentScope { return planbrief.ContentScope{Roles: []string{"engineer"}} }

func TestParseTypedPlan_AllFieldsStrict(t *testing.T) {
	original, err := json.Marshal(typedFixture())
	if err != nil {
		t.Fatal(err)
	}
	got, err := planbrief.ParseTypedPlan(original, typedScope())
	if err != nil || !reflect.DeepEqual(got, typedFixture()) {
		t.Fatalf("valid complete content: %v, %v", got, err)
	}
	// Each entry targets a distinct schema field, including nested collection items.
	fields := []struct{ key, value string }{
		{"summary", `"Plan summary"`}, {"stories", `[`}, {"key", `"US-001"`}, {"intent", `"create"`}, {"title", `"Typed story"`},
		{"user_story", `{`}, {"actor", `"owner"`}, {"want", `"safe changes"`}, {"benefit", `"clear requirements"`}, {"notes", `[`},
		{"acceptance_criteria", `[`}, {"id", `"AC-001"`}, {"text", `"A result"`}, {"tasks", `[`}, {"description", `"Implement"`}, {"owner_role", `"engineer"`}, {"criterion_ids", `[`}, {"depends_on", `[`},
		{"architecture", `{`}, {"components", `[`}, {"name", `"parser"`}, {"responsibility", `"validate"`}, {"interfaces", `[`}, {"input", `"JSON"`}, {"output", `"content"`}, {"constraints", `[`}, {"decisions", `[`}, {"decision", `"Typed fields"`}, {"reason", `"Explicit contract"`}, {"dependencies", `{`}, {"story_keys", `[`}, {"issue_refs", `[`},
	}
	for _, field := range fields {
		t.Run(field.key, func(t *testing.T) {
			t.Parallel()
			// Rename rather than remove a complex value: this independently exercises missing and unknown key detection.
			prefix := `"` + field.key + `":` + field.value
			malformed := strings.Replace(string(original), prefix, `"unexpected_`+field.key+`":`+field.value, 1)
			if _, err := planbrief.ParseTypedPlan([]byte(malformed), typedScope()); err == nil || !strings.Contains(err.Error(), field.key) {
				t.Fatalf("missing/unknown %s accepted or unlocated: %v", field.key, err)
			}
		})
	}
	for _, tt := range []struct{ name, from, to string }{
		{"null object", `"user_story":{`, `"user_story":null,"ignored":{`},
		{"null collection", `"notes":["A note"]`, `"notes":null`},
		{"null item", `"notes":["A note"]`, `"notes":[null]`},
		{"wrong string type", `"actor":"owner"`, `"actor":123`},
		{"wrong collection type", `"notes":["A note"]`, `"notes":"A note"`},
		{"wrong object type", `"user_story":{`, `"user_story":[],"ignored":{`},
		{"duplicate nested key", `"actor":"owner"`, `"actor":"owner","actor":"owner"`},
		{"duplicate root key", `"summary":"Plan summary"`, `"summary":"Plan summary","summary":"Plan summary"`},
		{"blank required narrative", `"actor":"owner"`, `"actor":" "`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := strings.Replace(string(original), tt.from, tt.to, 1)
			if _, err := planbrief.ParseTypedPlan([]byte(data), typedScope()); err == nil {
				t.Fatal("accepted malformed content")
			}
		})
	}
}

func TestTypedPlan_IdentityAndGraphValidation(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*planbrief.TypedPlan)
	}{
		{"no criteria", func(p *planbrief.TypedPlan) { p.Stories[0].AcceptanceCriteria = []planbrief.CriterionContent{} }},
		{"no tasks", func(p *planbrief.TypedPlan) { p.Stories[0].Tasks = []planbrief.TaskContent{} }},
		{"duplicate criterion", func(p *planbrief.TypedPlan) {
			p.Stories[0].AcceptanceCriteria = append(p.Stories[0].AcceptanceCriteria, p.Stories[0].AcceptanceCriteria[0])
		}},
		{"unknown role", func(p *planbrief.TypedPlan) { p.Stories[0].Tasks[0].OwnerRole = "invented" }},
		{"unknown criterion", func(p *planbrief.TypedPlan) { p.Stories[0].Tasks[0].CriterionIDs = []string{"AC-002"} }},
		{"unknown task", func(p *planbrief.TypedPlan) { p.Stories[0].Tasks[0].DependsOn = []string{"T-002"} }},
		{"task cycle", func(p *planbrief.TypedPlan) {
			task := p.Stories[0].Tasks[0]
			task.ID = "T-002"
			task.DependsOn = []string{"T-001"}
			p.Stories[0].Tasks[0].DependsOn = []string{"T-002"}
			p.Stories[0].Tasks = append(p.Stories[0].Tasks, task)
		}},
		{"unknown story", func(p *planbrief.TypedPlan) { p.Stories[0].Dependencies.StoryKeys = []string{"US-002"} }},
		{"unauthorized issue", func(p *planbrief.TypedPlan) { p.Stories[0].Dependencies.IssueRefs = []string{"github:someone/repo#1"} }},
		{"story cycle", func(p *planbrief.TypedPlan) {
			s := typedFixture().Stories[0]
			s.Key = "US-002"
			s.Dependencies.StoryKeys = []string{"US-001"}
			p.Stories[0].Dependencies.StoryKeys = []string{"US-002"}
			p.Stories = append(p.Stories, s)
		}},
		{"duplicate story", func(p *planbrief.TypedPlan) { p.Stories = append(p.Stories, p.Stories[0]) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := typedFixture()
			tt.edit(&p)
			if err := p.Validate(typedScope()); err == nil {
				t.Fatal("accepted invalid identity/graph")
			}
		})
	}
	p := typedFixture()
	s := typedFixture().Stories[0]
	s.Key = "US-002"
	s.Dependencies.StoryKeys = []string{"US-001"}
	p.Stories = append(p.Stories, s)
	if err := p.Validate(typedScope()); err != nil {
		t.Fatalf("valid DAG: %v", err)
	}
}

func TestValidCriterionID_CanonicalBoundaries(t *testing.T) {
	for _, tt := range []struct {
		id    string
		valid bool
	}{{"AC-001", true}, {"AC-099", true}, {"AC-100", true}, {"AC-999999", true}, {"AC-1000", true}, {"AC-000", false}, {"AC-1", false}, {"AC-01", false}, {"AC-0001", false}, {"AC-0100", false}, {"AC-1000000", false}, {"ac-001", false}, {"AC-001 ", false}, {"AC-１２３", false}} {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			if got := planbrief.ValidCriterionID(tt.id); got != tt.valid {
				t.Fatalf("got %v want %v", got, tt.valid)
			}
		})
	}
}

func TestTypedPlan_UTF8AndCountBudgets(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		count      int
		valid      bool
	}{
		{"exact UTF8 bytes", strings.Repeat("é", 2048), 1, true},
		{"UTF8 over limit", strings.Repeat("é", 2048) + "x", 1, false},
		{"invalid UTF8", string([]byte{0xff}), 1, false},
		{"256 criteria", "A result", 256, true},
		{"257 criteria", "A result", 257, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := typedFixture()
			p.Stories[0].AcceptanceCriteria = nil
			for i := 1; i <= tt.count; i++ {
				p.Stories[0].AcceptanceCriteria = append(p.Stories[0].AcceptanceCriteria, planbrief.CriterionContent{ID: fmt.Sprintf("AC-%03d", i), Text: tt.text})
			}
			err := p.Validate(typedScope())
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v err=%v", tt.valid, err)
			}
		})
	}
}

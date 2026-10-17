package planbrief_test

import (
	"encoding/json"
	"html"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

func reconciliationFixture(t *testing.T) (planbrief.TypedStory, string) {
	t.Helper()
	s := typedFixture().Stories[0]
	s.AcceptanceCriteria = []planbrief.CriterionContent{{ID: "AC-001", Text: "String must be x"}, {ID: "AC-002", Text: "Keep an audit record"}}
	body, err := report.RenderTypedStory(s, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	return s, body
}

func TestReconcileTypedStory_AllSections(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		edit           func(*planbrief.TypedStory)
	}{
		{"user story", "As owner,", "As operator,", func(s *planbrief.TypedStory) { s.UserStory.Actor = "operator" }},
		{"criterion replacement", "String must be x", "String must be y", func(s *planbrief.TypedStory) {
			s.AcceptanceCriteria[0] = planbrief.CriterionContent{ID: "AC-003", Text: "String must be y"}
			s.Tasks[0].CriterionIDs = []string{"AC-003"}
		}},
		{"tasks", "| Implement |", "| Add logging |", func(s *planbrief.TypedStory) { s.Tasks[0].Description = "Add logging" }},
		{"architecture", "Pure functions", "Use structured logging", func(s *planbrief.TypedStory) { s.Architecture.Summary = "Use structured logging" }},
		{"dependencies", "Story keys: None.", "Story keys: US-002", func(s *planbrief.TypedStory) { s.Dependencies.StoryKeys = []string{"US-002"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prior, body := reconciliationFixture(t)
			body = strings.Replace(body, tt.from, tt.to, 1)
			want, _ := reconciliationFixture(t)
			tt.edit(&want)
			got, err := planbrief.ReconcileTypedStory(prior, prior.Title, body, nil, typedScope())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Story, want) {
				t.Fatalf("content got %#v want %#v", got.Story, want)
			}
			if got.PresentationOnly {
				t.Fatal("semantic edit considered presentation only")
			}
			if !planbrief.TypedStoryBodyMatches(got.Story, got.Body) {
				t.Fatal("rewritten body does not match reconciled task/criterion references")
			}
		})
	}
}

func TestReconcileTypedStory_StableIdentity(t *testing.T) {
	for _, tt := range []struct {
		name         string
		edit         func(string) string
		used         []string
		ids, retired []string
		presentation bool
	}{
		{"reorder", func(b string) string {
			return strings.Replace(b, "- [ ] AC-001: String must be x\n- [ ] AC-002: Keep an audit record", "- [x] AC-002: Keep an audit record\n- [ ] AC-001: String must be x", 1)
		}, nil, []string{"AC-002", "AC-001"}, []string{}, true},
		{"multiline continuation", func(b string) string { return strings.Replace(b, "String must be x", "String must be\n  x", 1) }, nil, []string{"AC-001", "AC-002"}, []string{}, true},
		{"new items skip history", func(b string) string {
			return strings.Replace(b, "\n## Tasks", "\n- [ ] Return a receipt\n- [ ] Retain the request\n\n## Tasks", 1)
		}, []string{"AC-010"}, []string{"AC-001", "AC-002", "AC-011", "AC-012"}, []string{}, false},
		{"remove retires", func(b string) string { return strings.Replace(b, "- [ ] AC-002: Keep an audit record\n", "", 1) }, nil, []string{"AC-001"}, []string{"AC-002"}, false},
		{"replacement skips retired", func(b string) string { return strings.Replace(b, "String must be x", "String must be y", 1) }, []string{"AC-009"}, []string{"AC-010", "AC-002"}, []string{"AC-001"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prior, body := reconciliationFixture(t)
			before, _ := json.Marshal(prior)
			got, err := planbrief.ReconcileTypedStory(prior, prior.Title, tt.edit(body), tt.used, typedScope())
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, c := range got.Story.AcceptanceCriteria {
				ids = append(ids, c.ID)
			}
			if !reflect.DeepEqual(ids, tt.ids) || !reflect.DeepEqual(got.RetiredIDs, tt.retired) || got.PresentationOnly != tt.presentation {
				t.Fatalf("ids=%v retired=%v presentation=%v", ids, got.RetiredIDs, got.PresentationOnly)
			}
			after, _ := json.Marshal(prior)
			if string(before) != string(after) {
				t.Fatal("frozen prior mutated")
			}
		})
	}
}

func TestReconcileTypedStory_Pending(t *testing.T) {
	for _, tt := range []struct {
		name   string
		edit   func(string) string
		used   []string
		reason string
	}{
		{"ambiguous wording", func(b string) string {
			return strings.Replace(b, "Keep an audit record", "Log every successful result", 1)
		}, nil, "clarify"},
		{"retired ID reuse", func(b string) string {
			return strings.Replace(b, "\n## Tasks", "\n- [ ] AC-003: New result\n\n## Tasks", 1)
		}, []string{"AC-003"}, "retired"},
		{"duplicate ID", func(b string) string { return strings.Replace(b, "AC-002:", "AC-001:", 1) }, nil, "more than once"},
		{"missing section", func(b string) string { return strings.Replace(b, "## Architecture", "## Owner architecture", 1) }, nil, "missing Architecture"},
		{"contradictory dependencies", func(b string) string {
			return strings.Replace(b, "Story keys: None.", "Story keys: None.\nStory keys: US-002", 1)
		}, nil, "contradictory"},
		{"unknown task criterion", func(b string) string { return strings.Replace(b, "| AC&#45;001 |", "| AC-099 |", 1) }, nil, "unknown or duplicate criterion"},
		{"ID exhaustion", func(b string) string { return strings.Replace(b, "\n## Tasks", "\n- [ ] A new result\n\n## Tasks", 1) }, []string{"AC-999999"}, "no unused"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prior, body := reconciliationFixture(t)
			got, err := planbrief.ReconcileTypedStory(prior, prior.Title, tt.edit(body), tt.used, typedScope())
			if err == nil || !strings.Contains(err.Error(), "pending:") || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("got %+v, %v; want pending %s", got, err, tt.reason)
			}
		})
	}
}

func TestReconcileTypedStory_OwnerProseAndFooter(t *testing.T) {
	prior, body := reconciliationFixture(t)
	prose := "Evidence contract: explain observations here, without adding obligations."
	unknown := "## Owner notes\n\nKeep this exact text | even when tables change.\n"
	body = strings.Replace(body, "\n## Tasks", "\n"+prose+"\n\n## Tasks", 1) + "\n" + unknown
	got, err := planbrief.ReconcileTypedStory(prior, prior.Title, body, nil, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != body || len(got.Story.AcceptanceCriteria) != 2 || !got.PresentationOnly {
		t.Fatalf("owner text changed or invented obligations: %+v", got)
	}
	rendered, err := report.RenderTypedStory(prior, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	preserved := planbrief.PreserveStoryText(body, rendered)
	if !strings.Contains(preserved, prose) || !strings.Contains(preserved, unknown) || !strings.HasSuffix(preserved, planbrief.StoryGuidance+"\n") {
		t.Fatalf("owner text/footer lost: %s", preserved)
	}
	if !planbrief.TypedStoryBodyMatches(prior, preserved) {
		t.Fatal("owner prose manufactured typed content")
	}
	withoutFooter := strings.Replace(body, planbrief.StoryGuidance, "", 1)
	restored, err := planbrief.ReconcileTypedStory(prior, prior.Title, withoutFooter, nil, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(restored.Body, planbrief.StoryGuidance) != 1 || len(restored.Story.AcceptanceCriteria) != 2 {
		t.Fatal("missing footer not restored outside obligations")
	}
}

func TestReconcileTypedStory_ReorderedTasksPreserveFrozenOrder(t *testing.T) {
	prior, _ := reconciliationFixture(t)
	prior.Tasks = append(prior.Tasks, planbrief.TaskContent{ID: "T-002", Description: "Verify", OwnerRole: "engineer", CriterionIDs: []string{"AC-002"}, DependsOn: []string{"T-001"}})
	body, err := report.RenderTypedStory(prior, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(body, "\n")
	first, second := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "| T&#45;001 |") {
			first = i
		}
		if strings.HasPrefix(line, "| T&#45;002 |") {
			second = i
		}
	}
	if first < 0 || second < 0 {
		t.Fatal("task fixture rows missing")
	}
	lines[first], lines[second] = lines[second], lines[first]
	before, _ := json.Marshal(prior)
	got, err := planbrief.ReconcileTypedStory(prior, prior.Title, strings.Join(lines, "\n"), nil, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if !got.PresentationOnly || got.Story.Tasks[0].ID != "T-002" || !reflect.DeepEqual(got.Story.Tasks[0].DependsOn, []string{"T-001"}) {
		t.Fatalf("task reorder changed meaning: %+v", got)
	}
	after, _ := json.Marshal(prior)
	if string(before) != string(after) {
		t.Fatal("frozen task order was mutated")
	}
}

func TestPreserveStoryText_UnknownSubsections(t *testing.T) {
	prior, body := reconciliationFixture(t)
	note := "### Owner evidence\n\n- [ ] This is an owner's checklist, not a criterion.\n"
	source := strings.Replace(body, "\n## Tasks", "\n"+note+"\n## Tasks", 1)
	got := planbrief.PreserveStoryText(source, body)
	if !strings.Contains(got, note) || !planbrief.TypedStoryBodyMatches(prior, got) {
		t.Fatalf("unknown subsection changed obligations or lost text: %s", got)
	}
}

func TestReconcileTypedStory_ReplacementRemapsMultipleReferences(t *testing.T) {
	prior, _ := reconciliationFixture(t)
	prior.AcceptanceCriteria[1].Text = "Number must be 1"
	prior.Tasks[0].CriterionIDs = []string{"AC-001", "AC-002"}
	body, err := report.RenderTypedStory(prior, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	body = strings.Replace(body, "String must be x", "String must be y", 1)
	body = strings.Replace(body, "Number must be 1", "Number must be 2", 1)
	got, err := planbrief.ReconcileTypedStory(prior, prior.Title, body, []string{"AC-004"}, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Story.Tasks[0].CriterionIDs, []string{"AC-005", "AC-006"}) || !reflect.DeepEqual(got.RetiredIDs, []string{"AC-001", "AC-002"}) {
		t.Fatalf("replacement references/history: %+v", got)
	}
	if !planbrief.TypedStoryBodyMatches(got.Story, got.Body) {
		t.Fatalf("written body and references disagree: %s", got.Body)
	}
}

// The renderer writes URLs as [url](<url>) links; an unchanged body, rendered
// or as Linear stores it decoded, reconciles back to the original text.
func TestReconcileTypedStory_URLLinksRoundTrip(t *testing.T) {
	s := typedFixture().Stories[0]
	s.AcceptanceCriteria = []planbrief.CriterionContent{{ID: "AC-001", Text: "See https://a.test/x, [doc](https://b.test/y_z) and https://c.test/<b>."}}
	body, err := report.RenderTypedStory(s, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "[https://a.test/x](<https://a.test/x>)") || !strings.Contains(body, "[doc](<https://b.test/y_z>)") {
		t.Fatalf("links not rendered: %s", body)
	}
	for _, b := range []string{body, html.UnescapeString(body)} {
		got, err := planbrief.ReconcileTypedStory(s, s.Title, b, nil, typedScope())
		if err != nil || !got.PresentationOnly || got.Story.AcceptanceCriteria[0].Text != s.AcceptanceCriteria[0].Text {
			t.Fatalf("round trip = %q, %v", got.Story.AcceptanceCriteria, err)
		}
	}
}

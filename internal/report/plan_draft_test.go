package report_test

import (
	"encoding/json"
	"html"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

func renderDraftFixture() planbrief.PlanDraft {
	return planbrief.PlanDraft{Key: "story-a", Name: "Story Name", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "shared understanding"}, Problem: "Unclear requirements", InScope: []string{"Story conversation"}, NotInScope: []string{"Implementation"}}
}

func TestRenderPlanDraft_InitialScaffold(t *testing.T) {
	wantBytes, err := os.ReadFile("testdata/plan_draft_initial.golden")
	if err != nil {
		t.Fatal(err)
	}
	want := string(wantBytes)
	for _, tt := range []struct {
		name            string
		parent, ordinal int64
		title           string
	}{{"first", 362, 1, "362-001: Story Name"}, {"another parent", 123, 2, "123-002: Story Name"}, {"over 999", 362, 1000, "362-1000: Story Name"}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			title, body, err := report.RenderPlanDraft(tt.parent, tt.ordinal, renderDraftFixture(), "story", planbrief.ContentScope{})
			if err != nil {
				t.Fatal(err)
			}
			visible, _, ok := strings.Cut(body, "<!-- lf:plan-content")
			if !ok || title != tt.title || visible != want {
				t.Fatalf("title=%q body=%q", title, visible)
			}
		})
	}
	for _, ids := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}} {
		if _, _, err := report.RenderPlanDraft(ids[0], ids[1], renderDraftFixture(), "story", planbrief.ContentScope{}); err == nil {
			t.Fatalf("invalid authority ids accepted: %v", ids)
		}
	}
}

func TestRenderPlanDraft_ArchitectureApplicability(t *testing.T) {
	for _, kind := range []string{"agent", "go", "frontend"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			d := renderDraftFixture()
			d.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Owner", Decision: "Reuse roles", Rationale: "Preserve responsibilities"}}}
			want := []string{"## Architecture - General\n\n| Area | Decision | Rationale |\n|---|---|---|\n| Owner | Reuse roles | Preserve responsibilities |"}
			if kind == "go" {
				d.Architecture.DataModel = []planbrief.DataDefinition{{Entity: "Story", Definition: "Stable key", Constraints: "Unique per parent"}}
				d.Architecture.ServiceDesign = []planbrief.ServiceDefinition{{Component: "Repository", Responsibility: "Persist", Access: "Store only"}}
				d.Architecture.SolutionDesign = []planbrief.SolutionDefinition{{Element: "Service", Contract: "Calls repository", Rationale: "Reuse"}}
				want = append(want, "## Data Model\n\n| Entity / Relationship | Definition | Constraints / Rationale |", "## Service Design\n\n| Layer / Component | Responsibility | Dependencies / Access |", "## Solution Design\n\n| Element | Collaboration / Contract | Rationale |")
			}
			if kind == "frontend" {
				d.Frontend = true
				d.Design = []string{"https://example.com/design"}
				want = append(want, "# Design\n\n- https://example.com/design")
			}
			_, body, err := report.RenderPlanDraft(362, 1, d, "technical", planbrief.ContentScope{})
			if err != nil {
				t.Fatal(err)
			}
			offset := 0
			for _, fragment := range want {
				i := strings.Index(html.UnescapeString(body)[offset:], fragment)
				if i < 0 {
					t.Fatalf("missing or reordered %q in %s", fragment, body)
				}
				offset += i + len(fragment)
			}
			if kind != "go" {
				for _, heading := range []string{"## Data Model", "## Service Design", "## Solution Design"} {
					if strings.Contains(body, heading) {
						t.Fatalf("irrelevant %s emitted", heading)
					}
				}
			}
			if kind != "frontend" && strings.Contains(body, "# Design\n") {
				t.Fatal("irrelevant design emitted")
			}
		})
	}
}

func TestRenderPlanDraft_MetadataAndEscaping(t *testing.T) {
	d := renderDraftFixture()
	d.Problem = "Text\n# Forged heading"
	d.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "A|B", Decision: "<script>", Rationale: "Safe\ncell"}}}
	d.Criteria = []planbrief.PlanCriterion{{ID: "AC-003", Text: "Observable result", ProofMethod: "Unit test"}}
	d.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Implement", OwnerRole: "engineer", CriterionIDs: []string{"AC-003"}}}
	d.Dependencies.StoryKeys = []string{"story-b"}
	_, body, err := report.RenderPlanDraft(362, 1, d, "acceptance", planbrief.ContentScope{Roles: []string{"engineer"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"\n# Forged heading", "| A|B |", "<script>", "Safe\ncell"} {
		if strings.Contains(body, unsafe) {
			t.Fatalf("unsafe content %q remained", unsafe)
		}
	}
	if !strings.Contains(body, "- [ ] AC-003: Observable result") {
		t.Fatal("stable criterion missing")
	}
	_, encoded, ok := strings.Cut(body, "<!-- lf:plan-content v=1 data=")
	if !ok {
		t.Fatal("semantic metadata missing")
	}
	encoded, _, _ = strings.Cut(encoded, " -->")
	var got planbrief.PlanDraft
	if err = json.Unmarshal([]byte(encoded), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("typed metadata lost: %#v", got)
	}
}

func TestRenderRetainedPlanDraft_BacktrackingPreservesSections(t *testing.T) {
	d := renderDraftFixture()
	d.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Roles", Decision: "Keep current roles", Rationale: "Reuse"}}}
	d.Guidance = []string{"agent-setup"}
	d.Frontend = true
	d.Design = []string{"https://example.com/layout"}
	d.Criteria = []planbrief.PlanCriterion{{ID: "AC-001", Text: "Observable result", ProofMethod: "Unit test"}}
	d.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Implement", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}}}
	scope := planbrief.ContentScope{Roles: []string{"engineer"}}
	_, complete, err := report.RenderPlanDraft(362, 1, d, "acceptance", scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"story", "technical"} {
		t.Run(stage, func(t *testing.T) {
			title, body, err := report.RenderRetainedPlanDraft(362, 1, d, stage, scope)
			if err != nil {
				t.Fatal(err)
			}
			if title != "362-001: Story Name" || body != complete {
				t.Fatal("backtracking reset settled visible or typed content")
			}
			if _, _, err = report.RenderPlanDraft(362, 1, d, stage, scope); err == nil {
				t.Fatal("initial stage validator weakened")
			}
		})
	}
}

func TestRenderPlanDraft_BudgetCountsHiddenMetadata(t *testing.T) {
	d := renderDraftFixture()
	d.Problem = strings.Repeat("x", 40000)
	if err := d.Validate("story", planbrief.ContentScope{}); err != nil {
		t.Fatalf("fixture must pass the raw-draft check: %v", err)
	}
	if _, _, err := report.RenderPlanDraft(362, 1, d, "story", planbrief.ContentScope{}); err == nil || !strings.Contains(err.Error(), "exceeds byte budget") {
		t.Fatalf("draft repeated as metadata rendered past the budget: %v", err)
	}
}

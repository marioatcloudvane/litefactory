package planbrief_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

func editFixture(t *testing.T, kind string) (planbrief.PlanDraft, string, string) {
	t.Helper()
	d := draftFixture("acceptance")
	d.Dependencies = planbrief.DependencyContent{StoryKeys: []string{"dependency"}, IssueRefs: []string{}}
	d.Decisions = []string{"Keep settled choice"}
	d.Assumptions = []string{"Owner will reply"}
	if kind == "go" {
		d.Guidance = []string{"go"}
		d.Architecture.DataModel = []planbrief.DataDefinition{{Entity: "Story", Definition: "An intent", Constraints: "Stable key"}}
		d.Architecture.ServiceDesign = []planbrief.ServiceDefinition{{Component: "Store", Responsibility: "Own SQL", Access: "One connection"}}
		d.Architecture.SolutionDesign = []planbrief.SolutionDefinition{{Element: "Workflow", Contract: "Uses state interface", Rationale: "Isolation"}}
	}
	if kind == "frontend" {
		d.Frontend = true
		d.Design = []string{"https://example.com/layout"}
	}
	title, body, err := report.RenderPlanDraft(362, 1, d, "acceptance", planbrief.ContentScope{Roles: []string{"engineer"}})
	if err != nil {
		t.Fatal(err)
	}
	return d, title, "<!-- lf:plan-story parent=123 key=story-a -->\n<!-- lf:plan-request digest=abc -->\n" + body
}

func TestPlanDraftEdit_SemanticStageAndPresentation(t *testing.T) {
	tests := []struct {
		name, old, next, stage string
		title                  bool
	}{
		{"unchanged", "", "", "", false},
		{"blank line layout", "# Userstory\n\n", "# Userstory\n\n\n", "", false},
		{"manual checkbox", "- [ ] AC-001:", "- [x] AC-001:", "", false},
		{"intent", "I want clear intent", "I want revised intent", "story", false},
		{"problem", "Unclear requirements\n", "A revised problem\n", "story", false},
		{"scope", "- Story conversation\n", "- Story discussion\n", "story", false},
		{"story name", "Shape a story", "A clearer story", "story", true},
		{"architecture", "| Ownership | Separate roles |", "| Ownership | Reuse bounded roles |", "technical", false},
		{"criterion text", "AC-001: Draft retains intent", "AC-001: Draft retains intent after restart", "acceptance", false},
		{"task description", "Implement draft |", "Implement the draft reliably |", "acceptance", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prior, title, body := editFixture(t, "agent")
			if tt.old != "" {
				if tt.title {
					title = strings.Replace(title, tt.old, tt.next, 1)
				} else {
					body = strings.Replace(body, tt.old, tt.next, 1)
				}
			}
			got, err := planbrief.ReconcilePlanDraftEdit(prior, 362, 1, title, body, planbrief.ContentScope{Roles: []string{"engineer"}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Stage != tt.stage || got.PresentationOnly != (tt.stage == "") {
				t.Fatalf("stage=%q presentation=%v want %q", got.Stage, got.PresentationOnly, tt.stage)
			}
			if !reflect.DeepEqual(got.Draft.Dependencies, prior.Dependencies) || !reflect.DeepEqual(got.Draft.Tasks[0].CriterionIDs, prior.Tasks[0].CriterionIDs) || got.Draft.Criteria[0].ProofMethod != prior.Criteria[0].ProofMethod || !reflect.DeepEqual(got.Draft.Decisions, prior.Decisions) || !reflect.DeepEqual(got.Draft.Guidance, prior.Guidance) {
				t.Fatal("owner edit lost hidden semantic metadata")
			}
		})
	}
}

func TestPlanDraftEdit_RejectsAmbiguousEdits(t *testing.T) {
	tests := []struct {
		name   string
		change func(string, string) (string, string)
	}{
		{"renumbered title", func(title, body string) (string, string) {
			return strings.Replace(title, "362-001:", "362-002:", 1), body
		}},
		{"foreign prefix", func(title, body string) (string, string) {
			return strings.Replace(title, "362-001:", "363-001:", 1), body
		}},
		{"missing metadata", func(title, body string) (string, string) {
			body, _, _ = strings.Cut(body, "<!-- lf:plan-content")
			return title, body
		}},
		{"changed hidden dependency", func(title, body string) (string, string) {
			return title, strings.Replace(body, `"story_keys":["dependency"]`, `"story_keys":["foreign"]`, 1)
		}},

		{"changed hidden task identity", func(title, body string) (string, string) {
			return title, strings.Replace(body, `"id":"T-001"`, `"id":"T-002"`, 1)
		}},
		{"changed hidden criterion identity", func(title, body string) (string, string) {
			return title, strings.Replace(body, `"id":"AC-001"`, `"id":"AC-002"`, 1)
		}},
		{"changed hidden criterion link", func(title, body string) (string, string) {
			return title, strings.Replace(body, `"criterion_ids":["AC-001"]`, `"criterion_ids":["AC-002"]`, 1)
		}},
		{"new criterion without proof", func(title, body string) (string, string) {
			return title, strings.Replace(body, "AC-001: Draft retains intent", "AC-002: New behavior", 1)
		}},
		{"new task without links", func(title, body string) (string, string) {
			return title, strings.Replace(body, "| T&#45;001 |", "| T-002 |", 1)
		}},
		{"unknown heading", func(title, body string) (string, string) { return title, strings.Replace(body, "# Task", "# Tasks", 1) }},
		{"unknown prose", func(title, body string) (string, string) { return title, "Additional owner direction\n" + body }},
		{"unknown table column", func(title, body string) (string, string) {
			return title, strings.Replace(body, "| Owner | Dependencies |", "| Actor | Dependencies |", 1)
		}},
		{"raw ambiguous pipe", func(title, body string) (string, string) {
			return title, strings.Replace(body, "| Ownership |", "| Owner | ship |", 1)
		}},
		{"missing general", func(title, body string) (string, string) {
			return title, strings.Replace(body, "## Architecture - General", "## Data Model", 1)
		}},
		{"duplicate heading", func(title, body string) (string, string) {
			return title, strings.Replace(body, "# Task", "# Task\n# Task", 1)
		}},
		{"nonfrontend design", func(title, body string) (string, string) {
			return title, strings.Replace(body, "<!-- lf:plan-content", "# Design\n\n- https://example.com/layout\n\n<!-- lf:plan-content", 1)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prior, title, body := editFixture(t, "agent")
			title, body = tt.change(title, body)
			before := body
			got, err := planbrief.ReconcilePlanDraftEdit(prior, 362, 1, title, body, planbrief.ContentScope{Roles: []string{"engineer"}})
			if err == nil || got.Draft.Key != "" {
				t.Fatalf("ambiguous edit accepted: %+v %v", got, err)
			}
			if body != before {
				t.Fatal("rejected owner text was rewritten")
			}
		})
	}
}

func TestPlanDraftEdit_ArchitectureReuseAcrossFeedback(t *testing.T) {
	for _, kind := range []string{"agent", "go", "frontend"} {
		t.Run(kind, func(t *testing.T) {
			prior, title, body := editFixture(t, kind)
			body = strings.Replace(body, "I want clear intent", "I want refined intent", 1)
			scope := planbrief.ContentScope{Roles: []string{"engineer"}}
			first, err := planbrief.ReconcilePlanDraftEdit(prior, 362, 1, title, body, scope)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first.Draft.Architecture, prior.Architecture) || !reflect.DeepEqual(first.Draft.Design, prior.Design) {
				t.Fatal("earlier-stage feedback lost settled architecture/design")
			}
			raw, err := json.Marshal(first.Draft)
			if err != nil {
				t.Fatal(err)
			}
			var restored planbrief.PlanDraft
			if err = json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			body = strings.Replace(body, "I want refined intent", "I want refined intent again", 1)
			second, err := planbrief.ReconcilePlanDraftEdit(restored, 362, 1, title, body, scope)
			if err != nil {
				t.Fatal(err)
			}
			if second.Stage != "story" || !reflect.DeepEqual(second.Draft.Architecture, prior.Architecture) || !reflect.DeepEqual(second.Draft.Guidance, prior.Guidance) {
				t.Fatal("repeated feedback/restart reset settled content")
			}
		})
	}
}

func TestPlanDraftEdit_InitialScaffoldAndEscaping(t *testing.T) {
	d := draftFixture("story")
	d.Problem = "Pipes | entities & literal &#45;"
	d.InScope = []string{"A line\nand another"}
	title, body, err := report.RenderPlanDraft(362, 1000, d, "story", planbrief.ContentScope{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := planbrief.ReconcilePlanDraftEdit(d, 362, 1000, title, body, planbrief.ContentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.PresentationOnly || got.Draft.Architecture != nil || len(got.Draft.Tasks) != 0 || got.Draft.Problem != d.Problem || !reflect.DeepEqual(got.Draft.InScope, d.InScope) {
		t.Fatalf("initial scaffold did not roundtrip: %+v", got)
	}
}

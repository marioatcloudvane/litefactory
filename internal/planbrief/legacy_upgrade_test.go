package planbrief_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

const legacyUpgradeFixture = `<!-- lf:story task=github:owner/repo#1 story=US-001 -->

Owner introduction stays verbatim.

## User Story

As an owner, I want explicit upgrades, so that accepted work stays intact.

## Acceptance Criteria

- [ ] AC-001: Preview shows the exact issue revision.
- [x] AC-002: Apply requires explicit authorization.

## Tasks

| Task ID | Description | Agent | Size | Depends On | Status |
|---|---|---|---|---|---|
| T-001.1 | Build preview | engineer | M | — | TODO |
| T-001.2 | Build apply | engineer | M | T-001.1 | TODO |

## Architecture

Keep SQL in the store.
Preserve the existing workflow version.

## Dependencies

None.

## Evidence Inventory

Retain this owner evidence verbatim | even with punctuation.
`

func TestConvertLegacyStory_RecognizableContent(t *testing.T) {
	t.Parallel()
	got, err := planbrief.ConvertLegacyStory("US-001", "Explicit upgrade", legacyUpgradeFixture, nil, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "US-001" || got.Title != "Explicit upgrade" || got.Intent != "update" {
		t.Fatalf("identity: %+v", got)
	}
	if got.UserStory.Actor != "an owner" || got.UserStory.Want != "explicit upgrades" || got.UserStory.Benefit != "accepted work stays intact" {
		t.Fatalf("user story: %+v", got.UserStory)
	}
	wantCriteria := []planbrief.CriterionContent{{ID: "AC-001", Text: "Preview shows the exact issue revision."}, {ID: "AC-002", Text: "Apply requires explicit authorization."}}
	if !reflect.DeepEqual(got.AcceptanceCriteria, wantCriteria) {
		t.Fatalf("criteria: %+v", got.AcceptanceCriteria)
	}
	if len(got.Tasks) != 2 || !strings.HasPrefix(got.Tasks[0].Description, "Build preview") || got.Tasks[0].OwnerRole != "engineer" || !reflect.DeepEqual(got.Tasks[1].DependsOn, []string{"T-001.1"}) {
		t.Fatalf("tasks: %+v", got.Tasks)
	}
	if !strings.Contains(got.Architecture.Summary, "Keep SQL in the store.") || !strings.Contains(got.Architecture.Summary, "Preserve the existing workflow version.") {
		t.Fatalf("architecture lost: %+v", got.Architecture)
	}
	rendered, err := report.RenderTypedStory(got, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	preserved := planbrief.PreserveLegacyStoryText(legacyUpgradeFixture, rendered)
	for _, text := range []string{"Owner introduction stays verbatim.", "## Evidence Inventory\n\nRetain this owner evidence verbatim | even with punctuation.", planbrief.StoryGuidance} {
		if strings.Count(preserved, text) != 1 {
			t.Fatalf("owner content/footer not retained exactly once: %q\n%s", text, preserved)
		}
	}
	if !planbrief.TypedStoryBodyMatches(got, preserved) {
		t.Fatal("converted rendered body differs from typed content")
	}
}

func TestConvertLegacyStory_RefusesAmbiguity(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		dependencies   []string
	}{
		{"unnamed obligation", "- [ ] AC-001: Preview shows the exact issue revision.", "- [ ] Preview shows the exact issue revision.", nil},
		{"prose obligation", "\n## Tasks", "\nEvidence contract: Keep real evidence.\n\n## Tasks", nil},
		{"missing section", "## Architecture", "## Design commentary", nil},
		{"duplicate section", "## Architecture", "## User Story\n\n## Architecture", nil},
		{"duplicate criterion", "AC-002:", "AC-001:", nil},
		{"noncanonical criterion", "AC-001:", "AC-0001:", nil},
		{"unrecognized role", "| engineer |", "| invented |", nil},
		{"unknown prerequisite", "| T-001.1 | TODO |", "| T-099 | TODO |", nil},
		{"contradictory dependencies", "None.", "US-002", nil},
		{"omitted dependency", "None.", "None.", []string{"US-002"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(legacyUpgradeFixture, tt.from, tt.to, 1)
			_, err := planbrief.ConvertLegacyStory("US-001", "Upgrade", body, tt.dependencies, typedScope())
			if err == nil {
				t.Fatal("ambiguous or contradictory legacy conversion accepted")
			}
		})
	}
}

func TestConvertLegacyStory_DependencyIdentity(t *testing.T) {
	t.Parallel()
	body := strings.Replace(legacyUpgradeFixture, "None.", "US-003, US-002", 1)
	got, err := planbrief.ConvertLegacyStory("US-001", "Upgrade", body, []string{"US-002", "US-003"}, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Dependencies.StoryKeys, []string{"US-002", "US-003"}) {
		t.Fatalf("dependency identity lost: %+v", got.Dependencies)
	}
}

func TestPreserveLegacyStoryText_ExistingGuidance(t *testing.T) {
	t.Parallel()
	source := legacyUpgradeFixture + "\n## Story Guidance\n\n" + planbrief.StoryGuidance + "\n"
	s, err := planbrief.ConvertLegacyStory("US-001", "Upgrade", source, nil, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := report.RenderTypedStory(s, typedScope())
	if err != nil {
		t.Fatal(err)
	}
	got := planbrief.PreserveLegacyStoryText(source, rendered)
	if strings.Count(got, planbrief.StoryGuidance) != 1 || strings.Count(got, "## Story Guidance") != 1 {
		t.Fatalf("duplicated guidance: %s", got)
	}
	if !strings.HasSuffix(got, planbrief.StoryGuidance+"\n") {
		t.Fatal("guidance not at bottom")
	}
}

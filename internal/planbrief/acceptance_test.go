package planbrief

import (
	"reflect"
	"testing"
)

func TestAcceptanceCriteriaNamesEveryObligation(t *testing.T) {
	for _, tt := range []struct {
		name, section string
		want          []string
	}{
		{"prose", "Works.", []string{"Works."}},
		{"numbered", "1. First works.\n2) Second works.", []string{"First works.", "Second works."}},
		{"mixed", "- AC-1.1: First works.\n- Second works.", []string{"AC-1.1", "Second works."}},
		{"table", "| ID | Criterion |\n| --- | --- |\n| AC-1.1 | First works. |\n| **AC-1.2** | Second works. |", []string{"AC-1.1", "AC-1.2"}},
		{"checklist", "- [ ] AC-1: Works.\n* [x] Also works.", []string{"AC-1", "Also works."}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := "## User Story\nOwner.\n## Acceptance Criteria\n" + tt.section + "\n## Tasks\nBuild.\n## Architecture\nStore.\n## Dependencies\nNone.\n"
			if err := ValidateStoryBody(body); err != nil {
				t.Fatal(err)
			}
			got, err := AcceptanceCriteria(body)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestAcceptanceCriteriaRejectsAmbiguousOrEmptyObligations(t *testing.T) {
	for _, section := range []string{"", "| ID | Criterion |\n| --- | --- |", "| unnamed | silently lost |", "```\nAC-1 works\n```", "- AC-1: First.\n- AC-1: Second.", "- ", "## Acceptance Criteria\nAC-1: Duplicate section"} {
		if _, err := AcceptanceCriteria("## Acceptance Criteria\n" + section); err == nil {
			t.Fatalf("accepted %q", section)
		}
	}
}

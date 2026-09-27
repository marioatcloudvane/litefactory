package planbrief

import (
	"strings"
	"testing"
)

func TestProjectTypedAcceptanceExplicitFrozenIDs(t *testing.T) {
	frozen := "## Acceptance Criteria\n\n- [ ] AC-001: String must be x\nEvidence contract: explanation only\n\n## Tasks\nOriginal task\n"
	for _, tc := range []struct {
		name, live string
		fail       bool
	}{
		{"preserves narrative", frozen, false},
		{"preserves unrelated edits", strings.Replace(frozen, "Original task", "Owner task note", 1), false},
		{"idempotent checkbox", strings.Replace(frozen, "[ ]", "[x]", 1), false},
		{"changed requirement", strings.Replace(frozen, "must be x", "must be y", 1), true},
		{"unknown criterion", strings.Replace(frozen, "Evidence contract:", "- [ ] AC-002: new requirement\nEvidence contract:", 1), true},
		{"duplicate criterion", strings.Replace(frozen, "Evidence contract:", "- [ ] AC-001: duplicate\nEvidence contract:", 1), true},
		{"missing criterion", strings.Replace(frozen, "- [ ] AC-001: String must be x\n", "", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectTypedAcceptance(frozen, tc.live, []string{"AC-001"}, map[string]bool{"AC-001": true})
			if tc.fail {
				if err == nil {
					t.Fatal("conflicting requirements accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(tc.live, "- [ ] AC-001:", "- [x] AC-001:", 1)
			if got != want {
				t.Fatalf("projection changed unrelated content:\n%s", got)
			}
		})
	}
}

// Graph Plan renders the section under its level-1 PlanAcceptanceHeading and
// follows it with level-1 and level-2 architecture headings (#385's stories).
func TestProjectTypedAcceptanceGraphPlanLayout(t *testing.T) {
	frozen := "## Scope\n\n- [ ] not a criterion\n\n" + PlanAcceptanceHeading + "\n\n- [ ] AC-001: Bug is a distinct workflow&#46;\n- [ ] AC-002: Admission is stored&#46;\n\n# Architecture\n\n## Architecture - General\n\n- [ ] AC-003: architecture text, not a criterion\n"
	got, err := ProjectTypedAcceptance(frozen, frozen, []string{"AC-001", "AC-002"}, map[string]bool{"AC-001": true, "AC-002": false})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(frozen, "- [ ] AC-001:", "- [x] AC-001:", 1)
	if got != want {
		t.Fatalf("graph Plan projection changed other content:\n%s", got)
	}
	both := frozen + "\n## Acceptance Criteria\n\n- [ ] AC-001: again\n"
	if _, err := ProjectTypedAcceptance(frozen, both, []string{"AC-001", "AC-002"}, map[string]bool{"AC-001": true, "AC-002": false}); err == nil {
		t.Fatal("two acceptance sections accepted")
	}
}

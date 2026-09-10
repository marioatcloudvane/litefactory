package planbrief

import (
	"strings"
	"testing"
)

func TestProjectAcceptancePreservesRequirementsAndUnrelatedEdits(t *testing.T) {
	for _, tc := range []struct{ name, section string }{
		{"checklist", "- [ ] AC-1: First works.\n- [ ] AC-2: Second works."},
		{"table", "| ID | Criterion |\n| --- | --- |\n| AC-1 | First works. |\n| AC-2 | Second works. |"},
		{"numbered", "1. AC-1: First works.\n2. AC-2: Second works."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frozen := "## Acceptance Criteria\n" + tc.section + "\n## Tasks\nOriginal task.\n"
			live := strings.Replace(frozen, "Original task.", "Owner clarification outside acceptance.", 1)
			projected, err := ProjectAcceptance(frozen, live, map[string]bool{"AC-1": true, "AC-2": false})
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"First works.", "Second works.", "Owner clarification outside acceptance."} {
				if !strings.Contains(projected, text) {
					t.Fatalf("lost requirement or unrelated edit %q: %s", text, projected)
				}
			}
			if !strings.Contains(projected, "[x]") {
				t.Fatalf("accepted criterion not visibly checked: %s", projected)
			}
			again, err := ProjectAcceptance(frozen, projected, map[string]bool{"AC-1": true, "AC-2": false})
			if err != nil || again != projected {
				t.Fatalf("factory checkbox replay changed body: %q %v", again, err)
			}
		})
	}
}
func TestProjectAcceptanceRejectsRequirementConflicts(t *testing.T) {
	frozen := "## Acceptance Criteria\n- [ ] AC-1: First works.\n- [ ] AC-2: Second works.\n## Tasks\nBuild."
	for _, tc := range []struct{ name, live string }{
		{"changed text", strings.Replace(frozen, "First works.", "Different behavior.", 1)},
		{"removed criterion", strings.Replace(frozen, "- [ ] AC-2: Second works.\n", "", 1)},
		{"extra criterion", strings.Replace(frozen, "## Tasks", "- [ ] AC-3: New scope.\n## Tasks", 1)},
		{"duplicate criterion", strings.Replace(frozen, "AC-2", "AC-1", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ProjectAcceptance(frozen, tc.live, map[string]bool{"AC-1": true, "AC-2": true}); err == nil {
				t.Fatalf("conflicting scope projected: %s", got)
			}
		})
	}
}

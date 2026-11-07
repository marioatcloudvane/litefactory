package planbrief

import (
	"strings"
	"testing"
)

func TestValidateStoryBodyRequiresEverySectionWithinBudget(t *testing.T) {
	body := "## User Story\nAs owner.\n## Acceptance Criteria\nWorks.\n## Tasks\nBuild.\n## Architecture\nUse store.\n## Dependencies\nNone.\n"
	if err := ValidateStoryBody(body); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"User Story", "Acceptance Criteria", "Tasks", "Architecture", "Dependencies"} {
		t.Run(section, func(t *testing.T) {
			if err := ValidateStoryBody(strings.Replace(body, "## "+section, "## Removed", 1)); err == nil {
				t.Fatal("missing required section accepted")
			}
		})
	}
	t.Run("oversize", func(t *testing.T) {
		if err := ValidateStoryBody(body + strings.Repeat("x", MaxStoryBodyBytes)); err == nil {
			t.Fatal("oversize accepted")
		}
	})
}

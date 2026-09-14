package submission_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

// Incremental registration cannot accidentally enable an uninstalled family,
// including when the creation gate is enabled.
func TestFamilyCatalog_IncrementalInstallation(t *testing.T) {
	families := []struct{ name, effect string }{
		{"feedback_preflight", "read"}, {"issues_list", "read"}, {"issues_get", "read"},
		{"issues_comment", "publication"}, {"issues_create_batch", "creation"},
		{"evidence_submit", "checkpoint"}, {"activity_update", "advisory"},
		{"activity_get", "read"}, {"receipts_get", "read"},
	}
	for _, mode := range []string{"research", "implement", "validate", "plan", "spike"} {
		for _, installed := range families {
			t.Run(mode+"/"+installed.name, func(t *testing.T) {
				t.Parallel()
				profile, err := submission.NewFamilyToolProfile(mode, true, []string{installed.name})
				if err != nil {
					t.Fatal(err)
				}
				want := mode == "research" || mode == "implement" || mode == "validate"
				if installed.name == "issues_create_batch" {
					want = mode == "research"
				}
				if installed.name == "evidence_submit" {
					want = mode == "implement" || mode == "validate"
				}
				catalog := profile.List()
				expectedCount := 3
				if want {
					expectedCount++
				}
				if len(catalog) != expectedCount {
					t.Fatalf("catalog count=%d want=%d", len(catalog), expectedCount)
				}
				for _, family := range families {
					expected := want && family.name == installed.name
					if profile.Allows(family.name) != expected {
						t.Errorf("%s allowed=%v want=%v", family.name, profile.Allows(family.name), expected)
					}
				}
				for _, spec := range catalog {
					if spec.Name == installed.name && spec.Effect != installed.effect {
						t.Errorf("%s effect=%q want=%q", spec.Name, spec.Effect, installed.effect)
					}
				}
			})
		}
	}
}

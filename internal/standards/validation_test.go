package standards

import (
	"errors"
	"reflect"
	"testing"
)

func TestCanonicalDigestsAndReviewChanges(t *testing.T) {
	c := Category{ID: "c", Name: "Category"}
	d := Category{ID: "d", Name: "Other"}
	v := Standard{ID: "s", Rule: "MUST Title", Description: "Body", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "d", AppliesTo: []string{"story-worker", "plan-technical"}}
	ops := []Operation{{Kind: "standard.add", Standard: &v}, {Kind: "category.add", Category: &c}, {Kind: "category.add", Category: &d}}
	after, err := apply(Snapshot{}, ops)
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := changeDigest(0, ops)
	reversed := []Operation{ops[2], ops[0], ops[1]}
	if changeDigest(0, ops) != changeDigest(0, reversed) {
		t.Fatal("operation input order changed digest")
	}
	if changeDigest(1, ops) == changeDigest(0, ops) {
		t.Fatal("base revision absent from digest")
	}
	v.AppliesTo = []string{"plan-technical", "story-worker"}
	if changeDigest(0, ops) != originalDigest {
		t.Fatal("applies_to order changed digest")
	}
	cloned := canonicalOps(ops)
	v.Evidence = "Changed evidence"
	if changeDigest(0, ops) == originalDigest {
		t.Fatal("content absent from change digest")
	}
	if changeDigest(0, cloned) != originalDigest {
		t.Fatal("canonical operations alias caller content")
	}
	if len(diff(canonical(Snapshot{}), after)) != 3 {
		t.Fatal("incomplete review")
	}
	if !reflect.DeepEqual(after.Standards[0].AppliesTo, []string{"plan-technical", "story-worker"}) {
		t.Fatal("applies_to not canonical")
	}
}
func TestResultingSnapshotValidation(t *testing.T) {
	before := Snapshot{Categories: []Category{{ID: "c", Name: "C"}}, Standards: []Standard{{ID: "s", Rule: "MUST T", Description: "B", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "c", AppliesTo: []string{"story-worker"}}}}
	tests := []struct {
		name string
		ops  []Operation
	}{
		{"used category removal", []Operation{{Kind: "category.remove", ID: "c"}}},
		{"duplicate stable id", []Operation{{Kind: "category.add", Category: &Category{ID: "c", Name: "Other"}}}},
		{"duplicate operation", []Operation{{Kind: "category.remove", ID: "c"}, {Kind: "category.add", Category: &Category{ID: "c", Name: "Other"}}}},
		{"no change", nil},
		{"blank content", []Operation{{Kind: "standard.replace", Standard: &Standard{ID: "s", Rule: "MUST T", Description: " ", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "c", AppliesTo: []string{"story-worker"}}}}},
		{"unknown operation", []Operation{{Kind: "surprise"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := apply(before, tt.ops)
			var invalid *ValidationError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalid) || len(invalid.Problems) == 0 {
				t.Fatalf("expected actionable invalid result, got %v", err)
			}
		})
	}
	after, err := apply(before, []Operation{{Kind: "category.remove", ID: "c"}, {Kind: "category.add", Category: &Category{ID: "d", Name: "D"}}, {Kind: "standard.replace", Standard: &Standard{ID: "s", Rule: "MUST T", Description: "B", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "d", AppliesTo: []string{"story-worker"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Categories) != 1 || after.Categories[0].ID != "d" {
		t.Fatal("replacement failed")
	}
}

func TestStructuredStandardValidation(t *testing.T) {
	valid := Standard{ID: "SEC-001", CategoryID: "sec", Rule: "MUST store secrets as environment variables", Description: "Secrets never live in source.", Evidence: "The PR reads secrets from the environment.", Scope: "All services", Check: "semi_automated", AppliesTo: []string{"story-worker", "plan-technical"}}
	categories := []Category{{ID: "sec", Name: "Security"}}
	if err := validateSnapshot(Snapshot{Categories: categories, Standards: []Standard{valid}}); err != nil {
		t.Fatalf("valid standard refused: %v", err)
	}
	tests := map[string]struct {
		field string
		edit  func(*Standard)
	}{
		"rule without MUST": {"rule", func(s *Standard) { s.Rule = "SHOULD store secrets safely" }},
		"blank description": {"description", func(s *Standard) { s.Description = " " }},
		"blank evidence":    {"evidence", func(s *Standard) { s.Evidence = "" }},
		"blank scope":       {"scope", func(s *Standard) { s.Scope = "" }},
		"unknown check":     {"check", func(s *Standard) { s.Check = "sometimes" }},
		"unknown category":  {"category_id", func(s *Standard) { s.CategoryID = "missing" }},
		"no category":       {"category_id", func(s *Standard) { s.CategoryID = "" }},
		"no roles":          {"applies_to", func(s *Standard) { s.AppliesTo = nil }},
		"duplicate role":    {"applies_to", func(s *Standard) { s.AppliesTo = []string{"story-worker", "story-worker"} }},
		"blank role":        {"applies_to", func(s *Standard) { s.AppliesTo = []string{" "} }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := valid
			s.AppliesTo = append([]string{}, valid.AppliesTo...)
			tt.edit(&s)
			var invalid *ValidationError
			if err := validateSnapshot(Snapshot{Categories: categories, Standards: []Standard{s}}); !errors.As(err, &invalid) || len(invalid.Problems) == 0 || invalid.Problems[0].Field != "standards[SEC-001]."+tt.field {
				t.Fatalf("problem not reported on %s: %v", tt.field, err)
			}
		})
	}
}

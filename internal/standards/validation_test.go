package standards

import (
	"errors"
	"reflect"
	"testing"
)

func TestCanonicalDigestsAndReviewChanges(t *testing.T) {
	c := Category{ID: "c", Name: "Category"}
	d := Category{ID: "d", Name: "Other"}
	v := Standard{ID: "s", Title: "Title", Body: "Body", CategoryIDs: []string{"d", "c"}}
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
	v.CategoryIDs = []string{"c", "d"}
	if changeDigest(0, ops) != originalDigest {
		t.Fatal("membership order changed digest")
	}
	cloned := canonicalOps(ops)
	v.Body = "Changed body"
	if changeDigest(0, ops) == originalDigest {
		t.Fatal("content absent from change digest")
	}
	if changeDigest(0, cloned) != originalDigest {
		t.Fatal("canonical operations alias caller content")
	}
	if len(diff(canonical(Snapshot{}), after)) != 3 {
		t.Fatal("incomplete review")
	}
	if !reflect.DeepEqual(after.Standards[0].CategoryIDs, []string{"c", "d"}) {
		t.Fatal("membership not canonical")
	}
}
func TestResultingSnapshotValidation(t *testing.T) {
	before := Snapshot{Categories: []Category{{ID: "c", Name: "C"}}, Standards: []Standard{{ID: "s", Title: "T", Body: "B", CategoryIDs: []string{"c"}}}}
	tests := []struct {
		name string
		ops  []Operation
	}{
		{"used category removal", []Operation{{Kind: "category.remove", ID: "c"}}},
		{"duplicate stable id", []Operation{{Kind: "category.add", Category: &Category{ID: "c", Name: "Other"}}}},
		{"duplicate operation", []Operation{{Kind: "category.remove", ID: "c"}, {Kind: "category.add", Category: &Category{ID: "c", Name: "Other"}}}},
		{"no change", nil},
		{"blank content", []Operation{{Kind: "standard.replace", Standard: &Standard{ID: "s", Title: "T", Body: " ", CategoryIDs: []string{"c"}}}}},
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
	after, err := apply(before, []Operation{{Kind: "category.remove", ID: "c"}, {Kind: "category.add", Category: &Category{ID: "d", Name: "D"}}, {Kind: "standard.replace", Standard: &Standard{ID: "s", Title: "T", Body: "B", CategoryIDs: []string{"d"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Categories) != 1 || after.Categories[0].ID != "d" {
		t.Fatal("replacement failed")
	}
}

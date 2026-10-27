package standards_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
)

func TestPublishedBookCallerCompatibility(t *testing.T) {
	runtime, db := runtimeFixture(t)
	service := standards.Service{Store: db}
	revision, err := service.Revision(t.Context(), 0)
	if err != nil || revision.ID == 0 {
		t.Fatalf("revision: %+v %v", revision, err)
	}
	before, err := runtime.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	categories, err := runtime.CategoriesForRole(t.Context(), "story-worker")
	if err != nil || len(categories.Categories) != 1 || categories.Categories[0].ID != "go" || categories.Categories[0].Count != 1 {
		t.Fatalf("categories: %+v %v", categories, err)
	}
	var body strings.Builder
	cursor := ""
	for n := 0; n < 100; n++ {
		page, err := runtime.ForRole(t.Context(), "story-worker", []string{"go"}, cursor)
		if err != nil || page.Start != body.Len() || page.Status.Binding.Digest != revision.Digest {
			t.Fatalf("page: %+v %v", page, err)
		}
		body.WriteString(page.Text)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if !strings.Contains(body.String(), "MUST Rule") || !strings.Contains(body.String(), strings.Repeat("é", 6000)) {
		t.Fatal("rendered standards changed")
	}
	if _, err = runtime.ForRole(t.Context(), "story-worker", []string{"testing"}, ""); !errors.Is(err, standards.ErrUnknownCategory) {
		t.Fatalf("empty role category accepted: %v", err)
	}
	after, err := runtime.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.Binding.Digest != before.Status.Binding.Digest || len(after.Selections) != len(before.Selections) {
		t.Fatal("read changed binding or selection")
	}
}

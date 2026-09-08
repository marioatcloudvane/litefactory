package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestCompleteStoryPreservesDetailOrRefuses(t *testing.T) {
	rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	story := testStory()
	story.Excerpt = strings.Repeat("A complete task with acceptance and architecture.\n", 600)
	sub, err := rep.RenderCompleteStory(t.Context(), story)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sub.Body.String(), story.Excerpt) {
		t.Fatal("complete detail was dropped")
	}
	if strings.Contains(sub.Body.String(), "blob/") {
		t.Fatal("source-file link leaked into complete story")
	}
	story.Excerpt = strings.Repeat("unabridged ", 7000)
	if _, err := rep.RenderCompleteStory(t.Context(), story); err == nil {
		t.Fatal("oversize story silently truncated")
	}
}

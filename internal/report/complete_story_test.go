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

// TestRenderCompleteStoryOmitsReplyAdvisory ensures managed story publication
// keeps its own tracked guidance without the comment-only reply advisory.
func TestRenderCompleteStoryOmitsReplyAdvisory(t *testing.T) {
	rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	story := testStory()

	sub, err := rep.RenderCompleteStory(t.Context(), story)
	if err != nil {
		t.Fatal(err)
	}
	body := sub.Body.String()

	if strings.Contains(body, report.ReplyPlainlyNotice) {
		t.Errorf("RenderCompleteStory's body carries the legacy reply advisory:\n%s", body)
	}
	if strings.Contains(body, "Replying?") {
		t.Errorf("RenderCompleteStory's body carries a %q advisory sentence:\n%s", "Replying?", body)
	}
}

// testStory supplies complete inline content for the live publication renderer.
func testStory() report.Story {
	return report.Story{
		Ref:     testRef,
		ID:      "C1",
		Title:   "The plan ships as one GitHub sub-issue per user story",
		Excerpt: "| ID | Title | Type |\n|----|-------|------|\n| T-C1.3 | Create-issue capability, via the SDK | Go |\n",
	}
}

func TestRenderCompleteStoryPreservesPublicationIdentity(t *testing.T) {
	t.Parallel()
	story := testStory()
	sub, err := newReporter(t).RenderCompleteStory(t.Context(), story)
	if err != nil {
		t.Fatal(err)
	}
	want := report.StoryMarker(testRef, story.ID)
	if sub.Marker != want || !strings.HasPrefix(sub.Body.String(), want+"\n") {
		t.Fatalf("publication identity = %q, body = %q", sub.Marker, sub.Body.String())
	}
	if !sub.Body.Valid() || !sub.Title.Valid() || sub.Title.String() != story.Title {
		t.Fatalf("invalid publication title/body: %+v", sub)
	}
	if strings.Count(sub.Body.String(), report.MarkerPrefix) != 1 {
		t.Fatal("publication body must carry exactly one identity marker")
	}
}

func TestRenderCompleteStoryRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		story report.Story
	}{
		{"missing parent/refused", report.Story{ID: "C1", Excerpt: "complete content"}},
		{"missing story/refused", report.Story{Ref: testRef, Excerpt: "complete content"}},
		{"injected marker/refused", report.Story{Ref: testRef, ID: "C1 -->", Excerpt: "complete content"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sub, err := newReporter(t).RenderCompleteStory(t.Context(), tc.story)
			if err == nil {
				t.Fatal("invalid identity accepted")
			}
			if sub.Body.Valid() || sub.Title.Valid() {
				t.Fatal("invalid identity produced postable output")
			}
		})
	}
}

func TestRenderCompleteStoryRedactsPublication(t *testing.T) {
	t.Parallel()
	const secret = "private-deployment-secret-value"
	rep := newReporter(t, func(o *redact.Options) { o.Environ = []string{"DEPLOY_TOKEN=" + secret} })
	story := testStory()
	story.Title = "Rotate " + secret
	story.Excerpt = "Replace " + secret + " in deployment."
	sub, err := rep.RenderCompleteStory(t.Context(), story)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, text string }{{"title", sub.Title.String()}, {"body", sub.Body.String()}} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.text, secret) || !strings.Contains(tc.text, "[redacted]") {
				t.Fatalf("unredacted %s: %q", tc.name, tc.text)
			}
		})
	}
}

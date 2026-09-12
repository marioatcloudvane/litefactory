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

// TestRenderCompleteStoryOmitsReplyAdvisory. turn.PublishPlan (the live
// sub-issue creation path, taken for both the legacy and typed content
// contracts) calls RenderCompleteStory, never the legacy RenderSubIssue that
// TestSubIssueTellsTheReaderHowToReply shows does append
// [report.ReplyPlainlyNotice]. A managed sub-issue is a durably tracked,
// forge-reconciled artefact rather than a one-shot comment, so it must not
// carry that advisory — a quote-reply guard written for a body nobody
// reconciles against.
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

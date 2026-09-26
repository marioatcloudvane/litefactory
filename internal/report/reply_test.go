package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestLegacyToolsRefusalIsActionableAndEchoesNoPayload(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyLegacyToolLabel})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	containsAll(t, "legacy refusal", text, "no longer reads", "Remove every", "resolved run mode", "Nothing was run")
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	if strings.Contains(text, "run=") {
		t.Error("a run-less reply carries a run attribute")
	}
	for _, forbidden := range []string{"Tools: <Profile>", "research-profile", "sentinel"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("reply contains retired or payload text %q:\n%s", forbidden, text)
		}
	}
}

// A comment written while Research worked between two questions is refused in
// plain words: it arrived while Research was working, was not used, and should
// be resent in reply to the open question (#378).
func TestGraphReplyBetweenQuestionsRefusalSaysNotUsedAndResend(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyGraphReplyBetweenQuestions})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	if strings.Contains(text, "run=") {
		t.Error("a run-less reply carries a run attribute")
	}
	containsAll(t, "between-questions refusal", text,
		"arrived while Research was still working",
		"before its current question was posted",
		"it was not used",
		"nothing was run for it",
		"post it again as a reply to the open question",
	)
}

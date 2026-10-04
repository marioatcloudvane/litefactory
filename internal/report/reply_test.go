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
		"arrived while the workflow was still working",
		"before its current question or review was posted",
		"it was not used",
		"nothing was run for it",
		"post it again as a reply to the open question or review",
	)
}

// A Bug comment that cannot be applied is answered through the mode-refusal
// path, which consumes it, and tells the owner how to approve.
func TestRenderReplyBugCommentNotApplied(t *testing.T) {
	rep := newReporter(t)
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyBugCommentNotApplied})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) || !strings.Contains(text, "/approve-bug analysis <revision>") || !report.ReplyBugCommentNotApplied.RefusesMode() {
		t.Fatalf("reply:\n%s", text)
	}
}

// A Research comment refused because the wait already holds another reply is
// answered neutrally through the mode-refusal path, which consumes it.
func TestRenderReplyGraphOwnerWaitRefused(t *testing.T) {
	rep := newReporter(t)
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyGraphOwnerWaitRefused})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) || !strings.Contains(text, "already handling another reply or question") || !strings.Contains(text, "nothing was run") {
		t.Fatalf("reply:\n%s", text)
	}
	kind := report.ReplyGraphOwnerWaitRefused
	if kind.String() != "graph_owner_wait_refused" || !kind.RefusesMode() {
		t.Fatalf("kind = %q, RefusesMode = %v", kind.String(), kind.RefusesMode())
	}
}

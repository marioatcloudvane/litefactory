package report_test

import (
	"github.com/cloudvane/litefactory/internal/report"
	"strings"
	"testing"
)

func TestWorkflowRefusalOffersObservedDestinationWithoutApproval(t *testing.T) {
	rep := newReporter(t)
	const link = "https://tickets.example/decision/current"
	for _, kind := range []report.ReplyKind{report.ReplyBugCommentNotApplied, report.ReplyGraphReplyBetweenQuestions, report.ReplyGraphOwnerWaitRefused, report.ReplyValidateCommentNotApplied, report.ReplyImplementFinishedValidated} {
		body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind, DecisionURL: link})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.String(), "]("+link+")") {
			t.Fatalf("%s has no offered decision destination: %s", kind, body.String())
		}
		body, err = rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.String(), "not confirmed or recoverable") || strings.Contains(body.String(), link) {
			t.Fatalf("%s invents unavailable guidance: %s", kind, body.String())
		}
	}
}

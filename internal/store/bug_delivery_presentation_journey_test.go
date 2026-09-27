package store_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestBugDeliveryClarificationCreatesDistinctReviewWithoutRepair(t *testing.T) {
	for _, request := range []string{"show analysis, full", "Why is ordering still wrong?"} {
		t.Run(request, func(t *testing.T) {
			db, _, child, forge, activity, in := prJourneySetup(t)
			out, e := activity.Prepare(t.Context(), db, in)
			if e != nil || out.Review == nil {
				t.Fatalf("initial %+v %v", out, e)
			}
			original := *out.Review
			prJourneyPublish(t, db, original)
			before, _, _, e := db.CurrentBugAnalysis(t.Context(), in.SessionID)
			if e != nil {
				t.Fatal(e)
			}
			body := "/bug-review " + original.Digest + " clarify " + request
			in.Operation = "clarification-feedback"
			in.ReviewDigest = original.Digest
			in.Reply = prJourneyReply(original, in, body)
			out, e = activity.Prepare(t.Context(), db, in)
			if e != nil || out.Outcome != "clarify" {
				t.Fatalf("feedback %+v %v", out, e)
			}
			presentation, e := turn.BugDeliveryPresentation(t.Context(), db, in.SessionID, child.ID)
			if e != nil || len(presentation) != 1 || presentation[0].Body != body {
				t.Fatalf("presentation %+v %v", presentation, e)
			}
			in.Operation = "clarification-review"
			in.Reply = nil
			in.ReviewDigest = ""
			in.Presentation = presentation
			out, e = activity.Prepare(t.Context(), db, in)
			if e != nil || out.Review == nil {
				t.Fatalf("new review %+v %v", out, e)
			}
			current := *out.Review
			if current.Digest == original.Digest || current.DeliveryKey == original.DeliveryKey || current.Evidence.Child.ID != child.ID {
				t.Fatal("clarification reused publication identity or changed test evidence")
			}
			if request == "show analysis, full" {
				if len(presentation[0].References) != 2 {
					t.Fatal("explicit evidence request unresolved")
				}
			} else {
				if len(presentation[0].References) != 0 || !strings.Contains(current.Body, "Unanswered") {
					t.Fatal("arbitrary question fabricated an answer")
				}
			}
			preserved, e := turn.BugDeliveryPresentation(t.Context(), db, in.SessionID, child.ID)
			if e != nil || len(preserved) != 1 || preserved[0].Body != body {
				t.Fatalf("history lost %+v %v", preserved, e)
			}
			after, _, _, e := db.CurrentBugAnalysis(t.Context(), in.SessionID)
			if e != nil || before.AnalysisUsed != after.AnalysisUsed || store.BugPRJourneyRepairCount(t, db, in.SessionID) != 1 || forge.creates != 1 {
				t.Fatal("clarification spent work or duplicated PR")
			}
		})
	}
}

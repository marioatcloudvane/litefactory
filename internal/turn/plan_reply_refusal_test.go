package turn

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// A child reply the review rules refuse is a settled refusal the daemon can
// answer on the sub-issue, and it changes no saved state.
func TestAdmitReplyChildRefusalIsAPlanReplyRefusal(t *testing.T) {
	f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent")
	s := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-009", Stage: "acceptance", Candidate: "plan-55-600", Approvals: map[string]string{}, Draft: planbrief.PlanDraft{Key: "story-009"}}}}
	// Earlier stages are approved, so the wait is one a command can satisfy;
	// a read clears a wait behind a stale earlier approval (#1160).
	for _, stage := range []string{"story", "technical"} {
		ref, err := s.ReviewRevision("story-009", stage)
		mustProgress(t, err)
		s.Stories[0].Approvals[stage] = ref
	}
	revision, err := s.ReviewRevision("story-009", "acceptance")
	mustProgress(t, err)
	s.Stories[0].Wait = &planbrief.PlanWait{ID: "wait", Stage: "acceptance", Candidate: "plan-55-600", Revision: revision, Kind: "review", IssueID: 3980}
	_, rev, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), rev, s)
	mustProgress(t, err)

	req := base
	reply := planbrief.PlanOwnerReply{ID: 77, OwnerID: 7, ActorID: 7, IssueID: 3980, Key: "story-009", Stage: "acceptance", Candidate: "plan-55-600", WaitID: "stale-wait", Body: "/approve-plan story-009 acceptance plan-55-600"}
	req.Trigger = Trigger{CommentID: 77, AuthorID: 7, Body: reply.Body, PlanReply: &reply}
	r := &planGraphRuntime{opts: f.opts, req: req, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue, ownerID: 7}

	err = r.admitReply(t.Context())
	var refusal *PlanReplyRefusal
	if !errors.As(err, &refusal) || refusal.Reason == "" {
		t.Fatalf("admitReply error = %v, want a PlanReplyRefusal", err)
	}
	if !strings.Contains(refusal.Reason, "different owner wait or sub-issue") || !strings.Contains(refusal.Reason, reply.Body) || strings.Contains(refusal.Reason, "may be") {
		t.Fatalf("refusal lacks current grounded guidance: %s", refusal.Reason)
	}
	after, _, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	if slices.Contains(after.ConsumedReplies, 77) || after.Stories[0].Wait == nil {
		t.Fatalf("a refusal changed the saved plan: %+v", after)
	}
}

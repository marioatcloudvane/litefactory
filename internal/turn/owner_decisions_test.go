package turn

import (
	"strings"
	"testing"
)

func TestValidatePublishedShorthandRetainsExactSourceAndRejectsMissingDisplay(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.say("/validate-check\tstory-002:AC-002 pass -- opened it")
	st := o.state()
	f, ok := st.finding("story-002:AC-002")
	if !ok || f.State != findingOwnerPassed {
		t.Fatalf("short check not applied: %+v notice=%s", f, o.notice())
	}
	wait, err := o.db.GetGraphOwnerWait(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The previous source binding is retained even after this report is replaced.
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var source int64
	// Verdict provenance gives the exact authenticated source comment.
	if f.Verdict != nil {
		source = f.Verdict.CommentID
	}
	if source < 1 {
		t.Fatal("manual verdict lost source")
	}
	b, err := o.db.GetOwnerDecisionBinding(t.Context(), session.TaskRef, source)
	if err != nil {
		t.Fatal(err)
	}
	if b.Body != "/validate-check\tstory-002:AC-002 pass -- opened it" || b.Display.TestedCommit != st.Commit || !strings.Contains(b.Normalized, b.Display.Revision) {
		t.Fatalf("bad binding: %+v", b)
	}
	// A different unpublished action cannot use a bare command without its display.
	missing := wait
	missing.ReplyID = source + 10000
	missing.ReplyBody = "/validate-push"
	missing.ReplyAuthorID = o.owner
	absent := newOwnerFixture(t)
	if _, _, err = ResolveValidateDecision(t.Context(), absent.opts, ActivityRequest{InstanceID: absent.instance.ID}, ValidateState{}, missing); err == nil {
		t.Fatal("bare command accepted without a published target")
	}
}

func TestPlanPublishedShortApprovalsResolveOnce(t *testing.T) {
	f, _, base, _ := graphPlanFixture(t, "go")
	executeGraphPlan(t, f, base)
	first := graphPlanState(t, f).Stories[0]
	exact := graphPlanApprove(t, f, base, base.Trigger.CommentID+1)
	exact.Trigger.Body = "/approve-plan"
	exact.Trigger.PlanReply.Body = "/approve-plan"
	exact.Command = ParseCommand(exact.Trigger.Body)
	executeGraphPlan(t, f, exact)
	after := graphPlanState(t, f).Stories[0]
	if after.Stage == first.Stage {
		t.Fatal("bare approval failed to advance the displayed stage")
	}
	binding, err := f.db.GetOwnerDecisionBinding(t.Context(), f.ref().String(), exact.Trigger.CommentID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Body != "/approve-plan" || !strings.Contains(binding.Normalized, first.Candidate) {
		t.Fatalf("source or exact candidate lost: %+v", binding)
	}
	latest, err := f.db.LatestOwnerDecisionDisplay(t.Context(), f.ref().String(), "/approve-plan")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Resolved {
		t.Fatal("new stage should remain unresolved")
	}
	// The old explicit reference is stale, while the new reference selects only
	// its exact stage; neither command copies the full candidate identifier.
	_, err = f.db.ResolveOwnerDecisionDisplay(t.Context(), f.ref().String(), "/approve-plan", binding.Display.Reference, 0)
	if err == nil {
		t.Fatal("old reference silently selected new stage")
	}
	next := graphPlanApprove(t, f, base, base.Trigger.CommentID+2)
	next.Trigger.Body = "/approve-plan " + latest.Reference
	next.Trigger.PlanReply.Body = next.Trigger.Body
	next.Command = ParseCommand(next.Trigger.Body)
	executeGraphPlan(t, f, next)
	if graphPlanState(t, f).Stories[0].Stage == after.Stage {
		t.Fatal("short reference did not advance exact stage")
	}
}

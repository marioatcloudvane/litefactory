package turn

import (
	"github.com/cloudvane/litefactory/internal/store"
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

func TestPlanReplyToSelectsExactPublishedStoryAndAssembledDecisions(t *testing.T) {
	for _, action := range []string{"/approve-plan", "/approve-plan-set"} {
		t.Run(action, func(t *testing.T) {
			f, _, _, _ := graphPlanFixture(t, "go")
			task := f.ref().String()
			old := store.OwnerDecisionDisplay{TaskRef: task, Action: action, Target: action + " story-001 story old", Revision: "old", PublicationID: 701, CreatedAt: implementClock}
			current := old
			current.Target = action + " story-001 story current"
			current.Revision = "current"
			current.PublicationID = 702
			mustProgress(t, f.db.PutOwnerDecisionDisplay(t.Context(), old))
			mustProgress(t, f.db.PutOwnerDecisionDisplay(t.Context(), current))
			if _, err := ResolvePlanDecision(t.Context(), f.db, task, 901, 42, action, implementClock); err == nil {
				t.Fatal("bare replacement command silently selected latest")
			}
			if _, err := ResolvePlanDecision(t.Context(), f.db, task, 902, 42, action, implementClock, old.PublicationID); err == nil {
				t.Fatal("stale reply-to silently retargeted")
			}
			got, err := ResolvePlanDecision(t.Context(), f.db, task, 903, 42, action, implementClock, current.PublicationID)
			mustProgress(t, err)
			if got != current.Target {
				t.Fatalf("reply-to resolved %q", got)
			}
			retained, err := f.db.GetOwnerDecisionBinding(t.Context(), task, 903)
			mustProgress(t, err)
			if retained.Body != action || retained.ReplyToCommentID != current.PublicationID || retained.Display.PublicationID != current.PublicationID {
				t.Fatalf("reply identity lost: %+v", retained)
			}
			newer := current
			newer.Revision = "newer"
			newer.Target = action + " story-001 story newer"
			newer.PublicationID = 703
			mustProgress(t, f.db.PutOwnerDecisionDisplay(t.Context(), newer))
			replay, err := ResolvePlanDecision(t.Context(), f.db, task, 903, 42, action, implementClock, current.PublicationID)
			mustProgress(t, err)
			if replay != got {
				t.Fatalf("replay expanded against new work: %q", replay)
			}
			if _, err := ResolvePlanDecision(t.Context(), f.db, task, 903, 42, action, implementClock, newer.PublicationID); err == nil {
				t.Fatal("changed reply-to identity accepted on replay")
			}
			if action == "/approve-plan" {
				other := newer
				other.Target = "/approve-plan story-002 story second"
				other.Revision = "second"
				other.PublicationID = 704
				mustProgress(t, f.db.PutOwnerDecisionDisplay(t.Context(), other))
				child, err := ResolvePlanStoryDecision(t.Context(), f.db, task, 904, 42, action, implementClock, "story-001", newer.PublicationID)
				mustProgress(t, err)
				if child != newer.Target {
					t.Fatalf("child explicit identity resolved %q", child)
				}
				if _, err := ResolvePlanStoryDecision(t.Context(), f.db, task, 905, 42, action, implementClock, "story-001", other.PublicationID); err == nil {
					t.Fatal("another story's publication selected for child")
				}
			}
		})
	}
}

func TestValidateReplyToResolvesReplacementAndPreservesReplayIdentity(t *testing.T) {
	o := newOwnerFixture(t)
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	mustProgress(t, err)
	st := ValidateState{InstanceID: o.instance.ID, Commit: strings.Repeat("a", 40), DeliveryCommit: strings.Repeat("b", 40), Revision: "222222222222"}
	old := store.OwnerDecisionDisplay{TaskRef: session.TaskRef, Action: ValidateReturnCommand, InstanceID: o.instance.ID, Target: "old-work", Revision: "111111111111", TestedCommit: strings.Repeat("c", 40), DeliveryCommit: strings.Repeat("c", 40), PublicationID: 801, CreatedAt: o.clock}
	current := old
	current.Target = validateDecisionTarget(st)
	current.Revision = st.Revision
	current.TestedCommit = st.Commit
	current.DeliveryCommit = st.DeliveryCommit
	current.PublicationID = 802
	mustProgress(t, o.db.PutOwnerDecisionDisplay(t.Context(), old))
	mustProgress(t, o.db.PutOwnerDecisionDisplay(t.Context(), current))
	wait := store.GraphOwnerWait{ReplyID: 1001, ReplyAuthorID: o.owner, ReplyBody: "/return -- Use ISO dates.", ClaimedAt: o.clock}
	req := ActivityRequest{InstanceID: o.instance.ID}
	if _, _, err := ResolveValidateDecision(t.Context(), o.opts, req, st, wait); err == nil {
		t.Fatal("bare replacement decision silently retargeted")
	}
	wait.ReplyToCommentID = old.PublicationID
	if _, _, err := ResolveValidateDecision(t.Context(), o.opts, req, st, wait); err == nil {
		t.Fatal("old replied-to work selected new target")
	}
	exact := wait
	exact.ReplyBody = "/return " + st.Revision + " -- Use ISO dates."
	if _, _, err := ResolveValidateDecision(t.Context(), o.opts, req, st, exact); err == nil {
		t.Fatal("explicit revision ignored stale reply-to identity")
	}
	wait.ReplyToCommentID = current.PublicationID
	command, binding, err := ResolveValidateDecision(t.Context(), o.opts, req, st, wait)
	mustProgress(t, err)
	if command.Revision != st.Revision || command.Guidance != "Use ISO dates." || binding.Display.DeliveryCommit != st.DeliveryCommit || binding.ReplyToCommentID != current.PublicationID {
		t.Fatalf("reply-to target or source lost: %+v %+v", command, binding)
	}
	newer := current
	newer.Target = "newer-work"
	newer.Revision = "333333333333"
	newer.PublicationID = 803
	mustProgress(t, o.db.PutOwnerDecisionDisplay(t.Context(), newer))
	replay, again, err := ResolveValidateDecision(t.Context(), o.opts, req, st, wait)
	mustProgress(t, err)
	if replay.Revision != command.Revision || again.Display.ID != binding.Display.ID {
		t.Fatal("replay changed exact work")
	}
	wait.ReplyToCommentID = newer.PublicationID
	if _, _, err := ResolveValidateDecision(t.Context(), o.opts, req, st, wait); err == nil {
		t.Fatal("edited replay reply identity accepted")
	}
	// A short explicit reference retains its priority even when transport points
	// at a different report, and is still checked for exact current freshness.
	wait.ReplyID++
	wait.ReplyBody = "/return " + store.OwnerDecisionReference(newer.TaskRef, newer.Action, newer.Target, newer.Revision) + " -- Use UTC."
	wait.ReplyToCommentID = old.PublicationID
	st.Revision = newer.Revision
	if command, _, err := ResolveValidateDecision(t.Context(), o.opts, req, st, wait); err != nil || command.Revision != newer.Revision {
		t.Fatalf("reference priority: %+v %v", command, err)
	}
}

func TestPlanGraphCarriesExplicitReplyToIntoPublishedResolver(t *testing.T) {
	f, _, base, _ := graphPlanFixture(t, "go")
	executeGraphPlan(t, f, base)
	first := graphPlanState(t, f).Stories[0]
	display, err := f.db.LatestOwnerDecisionDisplay(t.Context(), f.ref().String(), "/approve-plan")
	mustProgress(t, err)
	reply := graphPlanApprove(t, f, base, base.Trigger.CommentID+1)
	reply.Trigger.Body = "/approve-plan"
	reply.Trigger.ReplyToCommentID = display.PublicationID
	reply.Trigger.PlanReply.Body = reply.Trigger.Body
	reply.Command = ParseCommand(reply.Trigger.Body)
	executeGraphPlan(t, f, reply)
	if graphPlanState(t, f).Stories[0].Stage == first.Stage {
		t.Fatal("explicit reply-to did not advance the displayed stage")
	}
	binding, err := f.db.GetOwnerDecisionBinding(t.Context(), f.ref().String(), reply.Trigger.CommentID)
	mustProgress(t, err)
	if binding.ReplyToCommentID != display.PublicationID || binding.Display.ID != display.ID || !strings.Contains(binding.Normalized, first.Candidate) {
		t.Fatalf("Plan caller dropped explicit publication identity: %+v", binding)
	}
}

func TestValidateGraphCarriesExplicitReplyToIntoPublishedResolver(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	display, err := o.db.LatestOwnerDecisionDisplay(t.Context(), mustOwnerTask(t, o), ValidateCheckCommand)
	mustProgress(t, err)
	wait, err := o.db.GetGraphOwnerWait(t.Context(), o.instance.ID)
	mustProgress(t, err)
	delivery, err := o.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	comment := delivery.CommentID + 1000
	body := "/validate-check story-002:AC-002 pass -- opened it"
	_, err = o.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: o.instance.ID, OwnerID: o.owner, AuthorID: o.owner, CommentID: comment, QuestionCommentID: delivery.CommentID, ReplyToCommentID: display.PublicationID, TaskRef: mustOwnerTask(t, o), SourceRef: mustOwnerTask(t, o), Mode: "validate", Workflow: "validate", DeliveryKey: wait.DeliveryKey, Body: body, At: o.clock})
	mustProgress(t, err)
	o.toOwner()
	finding, ok := o.state().finding("story-002:AC-002")
	if !ok || finding.State != findingOwnerPassed {
		t.Fatalf("explicit reply not applied: %+v notice %s", finding, o.notice())
	}
	binding, err := o.db.GetOwnerDecisionBinding(t.Context(), mustOwnerTask(t, o), comment)
	mustProgress(t, err)
	if binding.ReplyToCommentID != display.PublicationID || binding.Display.ID != display.ID || binding.Body != body {
		t.Fatalf("Validate graph lost explicit publication: %+v", binding)
	}
}

func mustOwnerTask(t *testing.T, o *ownerFixture) string {
	t.Helper()
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	mustProgress(t, err)
	return session.TaskRef
}

package daemon_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestPlanChildrenBoundary_AuthenticatedRoutingAndRestartCursor(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"exact", "/approve-plan story story candidate"},
		{"wrong-story", "/approve-plan foreign story candidate"},
		{"stale", "/approve-plan story story old"},
		{"mixed", "/approve-plan story story candidate but change scope"},
	} {
		t.Run(tc.name, func(t *testing.T) { planChildrenRoutingCase(t, tc.body, false) })
	}
}

func TestPlanChildrenBoundary_LockedReplyRetriedAfterRestart(t *testing.T) {
	planChildrenRoutingCase(t, "/approve-plan story story candidate", true)
}

func planChildrenRoutingCase(t *testing.T, ownerBody string, locked bool) {
	t.Helper()
	f := newFixture(t)
	attempts := 0
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		attempts++
		if locked && attempts == 1 {
			return turn.Result{Disposition: turn.DispositionLocked}, nil
		}
		out, err := f.ranSuccessfully(ctx, req)
		out.ResearchGraph = true
		return out, err
	}
	f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent Plan", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
	f.settle()
	session := f.sessionOf(testIssue)
	if _, err := f.db.AdmitPlanPath(t.Context(), session.ID); err != nil {
		t.Fatal(err)
	}
	draft := planbrief.PlanDraft{Key: "story", Name: "Child", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "review"}, Problem: "Scope unclear", InScope: []string{"draft"}, NotInScope: []string{}}
	title, body, err := report.RenderPlanDraft(123, 1, draft, "story", planbrief.ContentScope{})
	if err != nil {
		t.Fatal(err)
	}
	body = "<!-- lf:plan-story parent=1230 key=story -->\n" + body
	binding, err := f.db.ReservePlanStory(t.Context(), session.TaskRef, draft.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.BindPlanStoryIssue(t.Context(), session.TaskRef, draft.Key, 2000, 200); err != nil {
		t.Fatal(err)
	}
	if err = f.db.PutPlanStoryCandidate(t.Context(), store.PlanStoryCandidate{Parent: session.TaskRef, Key: draft.Key, RequestID: "create", Candidate: "candidate", RequestJSON: `{"operation":"create"}`}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	binding.IssueID = 2000
	binding.IssueNumber = 200
	binding.Revision = turn.PlanIssueRevision(title, body)
	binding.Candidate = "candidate"
	binding.DraftJSON = string(raw)
	if err = f.db.ApplyPlanStoryDraft(t.Context(), binding, "", store.PlanStoryEffect{Parent: session.TaskRef, RequestID: "create", Phase: "attach", Outcome: "applied", IssueID: 2000, IssueNumber: 200, Revision: binding.Revision}); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared"}
	if err = state.PutCandidate(draft, "story", "candidate"); err != nil {
		t.Fatal(err)
	}
	if err = state.OpenPlanWait("story", "wait", "review", 2000); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	f.forge.addIssue(fakeIssue{ID: 2000, Number: 200, Title: title, Body: body, State: "open", User: stranger()})
	renamed := owner()
	renamed.Login = "renamed-owner"
	f.forge.addComment(200, 50, stranger(), "/approve-plan story story candidate")
	f.forge.addComment(200, 60, renamed, ownerBody)
	f.settle()
	if locked {
		cursor, cursorErr := f.db.StoryDiscoveryCursor(t.Context(), session.ID, 200)
		if cursorErr != nil || cursor >= 60 {
			t.Fatalf("locked reply consumed child cursor: %d %v", cursor, cursorErr)
		}
		if f.watermark() != 0 {
			t.Fatalf("locked child advanced parent watermark: %d", f.watermark())
		}
		f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
		f.settle()
	}
	calls := f.turns.calls()
	if ownerBody == "/approve-plan foreign story candidate" || ownerBody == "/approve-plan story story old" {
		if len(calls) != 0 {
			t.Fatalf("stale reply invoked executor: %+v", calls)
		}
		explanation, err := f.db.PlanReplyExplanation(t.Context(), session.TaskRef, 60)
		if err != nil || explanation.Reason == "" {
			t.Fatalf("missing stale explanation %+v %v", explanation, err)
		}
		delivery, err := f.db.GetReportDelivery(t.Context(), explanation.DeliveryKey)
		if err != nil || !delivery.Completed {
			t.Fatalf("unconfirmed explanation %+v %v", delivery, err)
		}
		cursor, err := f.db.StoryDiscoveryCursor(t.Context(), session.ID, 200)
		if err != nil || cursor != 60 || f.watermark() != 0 {
			t.Fatalf("unsafe cursor=%d err=%v parent=%d", cursor, err, f.watermark())
		}
		f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
		f.settle()
		if len(f.turns.calls()) != 0 {
			t.Fatal("restart dispatched stale reply")
		}
		return
	}
	expectedCalls := 1
	if locked {
		expectedCalls = 2
		if len(calls) == 2 && (calls[1].Trigger.PlanReply == nil || calls[1].Trigger.PlanReply.ID != 60) {
			t.Fatalf("retry lost original child reply: %+v", calls)
		}
	}
	if len(calls) != expectedCalls || calls[0].Mode != agent.ModePlan || calls[0].Issue.Number != testIssue || calls[0].Trigger.PlanReply == nil {
		t.Fatalf("child not routed into parent Plan: %+v logs=%s", calls, f.logs)
	}
	reply := calls[0].Trigger.PlanReply
	if reply.ID != 60 || reply.ActorID != ownerID || reply.OwnerID != ownerID || reply.IssueID != 2000 || reply.WaitID != "wait" || reply.Candidate != "candidate" {
		t.Fatalf("child correlation lost: %+v", reply)
	}
	if reply.Body != ownerBody || reply.Key != "story" || reply.Stage != "story" {
		t.Fatalf("free-text reply changed correlation: %+v", reply)
	}
	retained, _, err := f.db.GetPlanWorkflowState(t.Context(), session.TaskRef)
	if err != nil || len(retained.Stories) != 1 || retained.Stories[0].Stage != "story" || len(retained.Stories[0].Approvals) != 0 {
		t.Fatalf("discovery granted approval: %+v %v", retained, err)
	}
	if f.watermark() != 0 {
		t.Errorf("child reply advanced parent watermark to %d", f.watermark())
	}
	f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
	f.settle()
	if len(f.turns.calls()) != expectedCalls {
		t.Fatal("restart redispatched consumed child cursor")
	}
}

package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestStageDecisionPlanRevisionUsesRetainedChildDispatch(t *testing.T) {
	changing := false
	f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
		if changing {
			a.Request.Draft.Problem = "Clarified scope from the confirmed revision"
			a.Request.ChangedSections = []string{"intent"}
		}
	})
	executeGraphPlan(t, f, base)
	child := asStoryTask(t, f, nextTrigger(base, 9700, "Review the current story"), "story-001")
	executeGraphPlan(t, f, child)
	before := graphPlanState(t, f)
	session, err := f.db.GetSession(t.Context(), child.SessionID)
	mustProgress(t, err)
	boundary := StageDecisionBoundary{DB: f.db}
	target, err := boundary.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "story", "revise_task")
	mustProgress(t, err)
	proposal, ctx := confirmTurnStageAction(t, f, target)
	change := "Please clarify the scope. Then approve it."
	receipt, err := boundary.ApplyStageDecision(ctx, target, change, proposal.ExecutionID, proposal.ConfirmNotBefore)
	mustProgress(t, err)
	if receipt.State != "applied" {
		t.Fatalf("revision not admitted: %+v", receipt)
	}
	admitted := graphPlanState(t, f)
	if admitted.Stories[0].Candidate != before.Stories[0].Candidate || admitted.Stories[0].Wait != nil {
		t.Fatal("admission changed unseen candidate")
	}
	original := before.Stories[0]
	staleApproval := planbrief.PlanOwnerReply{ID: 9800, OwnerID: 7, ActorID: 7, IssueID: original.Wait.IssueID, Key: original.Key, Stage: original.Stage, Candidate: original.Candidate, WaitID: original.Wait.ID, Body: "/approve-plan " + original.Key + " " + original.Stage + " " + original.Candidate}
	approvalErr := f.db.UpdatePlanWorkflowState(store.WithPlanOwnerAdmission(t.Context(), target.PlanParent, target.StoryKey), target.PlanParent, func(s *planbrief.PlanState) (bool, error) {
		_, e := s.ApplyOwnerReply(staleApproval)
		return e == nil, e
	})
	if approvalErr == nil {
		t.Fatal("GitHub approved the candidate after chat consumed its revision wait")
	}
	id, err := strconv.ParseInt(receipt.ContinuationRef, 10, 64)
	mustProgress(t, err)
	dispatch, reserved, err := f.db.ReserveGraphDispatch(t.Context(), id, proposal.ConfirmNotBefore)
	mustProgress(t, err)
	if !reserved {
		t.Fatal("revision dispatch not reserved")
	}
	continuation, found, err := f.db.StageDecisionForDispatch(t.Context(), id)
	mustProgress(t, err)
	if !found || continuation.StoryKey != "story-001" {
		t.Fatalf("missing child continuation: %+v", continuation)
	}
	child.Trigger = Trigger{CommentID: dispatch.TriggerID(), StageDecisionRef: continuation.ExecutionID, Body: continuation.Change, AuthorID: continuation.OwnerID, Author: "owner"}
	child.Command = Command{Kind: KindFreeText, Text: change}
	changing = true
	executeGraphPlan(t, f, child)
	after := graphPlanState(t, f)
	item := after.Stories[0]
	if item.Candidate == before.Stories[0].Candidate || item.Draft.Problem != "Clarified scope from the confirmed revision" || item.Wait == nil || item.Stage != "story" {
		t.Fatalf("revision did not reopen same stage: %+v", item)
	}
	revision, err := after.ReviewRevision(item.Key, "story")
	mustProgress(t, err)
	if item.Approvals["story"] == revision {
		t.Fatal("mixed revision approved unseen work")
	}
}

func confirmTurnStageAction(t *testing.T, f *fixture, target store.StageDecisionTarget) (store.OwnerAction, context.Context) {
	t.Helper()
	owner, err := f.db.ConnectorOwner(t.Context(), 7)
	mustProgress(t, err)
	at := time.Now().UTC()
	facts, _ := json.Marshal(struct {
		TargetID       string `json:"target_id"`
		TargetRevision string `json:"target_revision"`
		Invocation     struct {
			Command string `json:"command"`
		} `json:"invocation"`
	}{TargetID: target.TaskRef, TargetRevision: target.Revision, Invocation: struct {
		Command string `json:"command"`
	}{Command: target.Operation}})
	payload, _ := json.Marshal(struct {
		Target store.StageDecisionTarget `json:"target"`
	}{Target: target})
	principal := store.OwnerActionPrincipal{OwnerID: "7", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "200", DiscordOwnerID: "100"}
	proposal := store.OwnerAction{ID: "plan-stage", PolicyVersion: 1, Principal: principal, RequestKey: "revision", RequestDigest: "revision-input", Facts: string(facts), Payload: string(payload), Digest: "reviewed", RepositoryID: target.RepositoryID, RepositoryRef: target.RepositoryRef, Scope: "repository:actions", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:plan-stage"}
	if target.Operation == "approve_stage" {
		proposal.Tier = "binding"
		proposal.ApprovalChannel = "200"
	}
	mustProgress(t, f.db.CreateOwnerAction(t.Context(), proposal))
	_, err = f.db.MutateOwnerAction(t.Context(), principal, proposal.ID, "confirm", "", proposal.ConfirmNotBefore)
	mustProgress(t, err)
	if proposal.Tier == "binding" {
		ok, e := f.db.ClaimOwnerActionDelivery(t.Context(), proposal.ID, proposal.ConfirmNotBefore)
		mustProgress(t, e)
		if !ok {
			t.Fatal("approval delivery not claimed")
		}
		mustProgress(t, f.db.FinishOwnerActionDelivery(t.Context(), proposal.ID, "approval-message", "", proposal.ConfirmNotBefore))
		_, e = f.db.MutateOwnerAction(t.Context(), principal, proposal.ID, "reaction", `{"message":"approval-message","emoji":"✅"}`, proposal.ConfirmNotBefore)
		mustProgress(t, e)
	}
	claimed, err := f.db.ClaimOwnerActionExecution(t.Context(), proposal.ID, proposal.ConfirmNotBefore)
	mustProgress(t, err)
	if !claimed {
		t.Fatal("execution not claimed")
	}
	ctx := store.WithOwnerActionEffect(t.Context(), proposal.ExecutionID, func() time.Time { return proposal.ConfirmNotBefore })
	return proposal, ctx
}

func TestStageDecisionPlanAssembledApprovalUsesRetainedScope(t *testing.T) {
	f, _, _, before := completeSingleStory(t)
	boundary := StageDecisionBoundary{DB: f.db}
	target, err := boundary.ResolveStageDecisionTarget(t.Context(), f.ref().String(), "assembled", "approve_stage")
	mustProgress(t, err)
	if len(target.Scope.Roles) == 0 {
		t.Fatal("retained approval scope missing")
	}
	proposal, ctx := confirmTurnStageAction(t, f, target)
	receipt, err := boundary.ApplyStageDecision(ctx, target, "", proposal.ExecutionID, proposal.ConfirmNotBefore)
	mustProgress(t, err)
	after := graphPlanState(t, f)
	if receipt.State != "applied" || after.AssembledApproval != before.AssembledWait || after.AssembledWait != "" {
		t.Fatalf("assembled approval differs: %+v %+v", receipt, after)
	}
}

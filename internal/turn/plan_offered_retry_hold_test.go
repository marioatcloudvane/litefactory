package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// heldLinearStoryForge serves the bound story as a Linear issue.
type heldLinearStoryForge struct {
	SubIssueCreator
	PlanStoryForge
	issue github.Issue
}

func (l heldLinearStoryForge) GetIssue(_ context.Context, _ github.Repo, number int) (github.Issue, error) {
	if number != l.issue.Number {
		return github.Issue{}, store.ErrNotFound
	}
	return l.issue, nil
}

// MAR-114 (#1521): after a false Linear owner-edit conflict, the owner posts the
// retry the failure notice offers on the story's issue. That `/plan …` reply
// is a command, not discussion, so it never reached the #862 release of such
// holds and the story stayed held for good.
func TestPlanRetry_OfferedRetryReleasesFalseLinearOwnerEditHold(t *testing.T) {
	f, _, _, _ := graphPlanFixture(t, "agent")
	ctx, parent := t.Context(), f.ref().String()
	draft := planbrief.PlanDraft{Key: "story-001", Name: "A usable story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "shape a story", Benefit: "retain intent"}, Problem: "Plans need discussion", InScope: []string{"Drafts"}, NotInScope: []string{"Implementation"}}
	shell := PlanIssueRevision("Story shell", "<!-- lf:plan-story parent=1 key=story-001 -->\nDraft pending")
	request := graphexec.PlanStoryRequest{RequestID: "plan-request-2238-23555", Operation: "update", ExpectedRevision: shell, Stage: "story", Candidate: "candidate-one", SharedRevision: "initial", Draft: draft}
	raw, err := json.Marshal(request)
	mustProgress(t, err)
	_, err = f.db.ReservePlanStory(ctx, parent, draft.Key)
	mustProgress(t, err)
	mustProgress(t, f.db.BindPlanStoryShell(ctx, parent, draft.Key, 2000, 200, shell))
	mustProgress(t, f.db.PutPlanStoryCandidate(ctx, store.PlanStoryCandidate{Parent: parent, RequestID: request.RequestID, Key: draft.Key, Candidate: request.Candidate, RequestJSON: string(raw)}))
	for _, e := range []struct{ outcome, detail string }{{"uncertain", "content readback differs"}, {"conflict", ownerEditConflict}} {
		mustProgress(t, f.db.AppendPlanStoryEffect(ctx, store.PlanStoryEffect{Parent: parent, RequestID: request.RequestID, Phase: "update", Outcome: e.outcome, IssueID: 2000, IssueNumber: 200, Detail: e.detail}))
	}
	state := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: draft.Key, Stage: "story", Candidate: request.Candidate, Draft: draft, Approvals: map[string]string{}, Effect: "conflict", EffectRequest: request.RequestID}}}
	state.Stories[0].SetEffectHold(request.RequestID, ownerEditConflict)
	_, current, err := f.db.GetPlanWorkflowState(ctx, parent)
	mustProgress(t, err)
	_, err = f.db.SavePlanWorkflowState(ctx, parent, current, state)
	mustProgress(t, err)

	creator := f.opts.SubIssueCreator
	forge, ok := creator.(PlanStoryForge)
	if !ok {
		t.Fatal("fixture forge lacks story capabilities")
	}
	opts := f.opts
	opts.SubIssueCreator = heldLinearStoryForge{SubIssueCreator: creator, PlanStoryForge: forge, issue: github.Issue{ID: 2000, Number: 200, State: github.StateOpen, Title: "Story one", Body: "landed draft with running-work", QualifiedSnapshot: &ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear}}}}}
	req := f.request()
	req.Trigger = Trigger{CommentID: 9991, Body: offeredPlanRetry, Author: "owner", AuthorID: 7, PlanReply: &planbrief.PlanOwnerReply{ID: 9991, OwnerID: 7, ActorID: 7, IssueID: 2000, Key: draft.Key, Body: offeredPlanRetry}}
	r := &planGraphRuntime{opts: opts, req: req, parentRef: f.ref(), story: draft.Key, ownerID: 7}
	retry, err := r.retryFailedStory(ctx)
	if err != nil || !retry {
		t.Fatalf("the offered retry did not retry the held story: retry=%v err=%v", retry, err)
	}
	saved, _, err := f.db.GetPlanWorkflowState(ctx, parent)
	mustProgress(t, err)
	if item := saved.Stories[0]; item.Blocked() {
		t.Fatalf("the false owner-edit hold was kept: %+v", item)
	}
	binding, err := f.db.GetPlanStoryDraft(ctx, parent, draft.Key)
	mustProgress(t, err)
	if binding.Revision != PlanIssueRevision("Story one", "landed draft with running-work") {
		t.Fatal("the landed content was not taken as the expected revision")
	}

	// A comment from anyone else is no owner retry and releases nothing.
	r.req.Trigger.AuthorID = 8
	if retry, err := r.retryFailedStory(ctx); err != nil || retry {
		t.Fatalf("a foreign comment retried: %v %v", retry, err)
	}
	if !strings.HasPrefix(offeredPlanRetry, "/plan ") {
		t.Fatal("offered retry changed")
	}
}

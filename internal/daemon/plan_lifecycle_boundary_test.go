package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Relationship reads still cross HTTP; individual children can point at a
// different observed parent without replacing daemon discovery or persistence.
type lifecycleBoundaryForge struct {
	*github.Client
	parents map[int]int
}

func (f lifecycleBoundaryForge) GetParentIssue(ctx context.Context, repo github.Repo, child int) (github.Issue, error) {
	parent := testIssue
	if n, ok := f.parents[child]; ok {
		parent = n
	}
	return f.GetIssue(ctx, repo, parent)
}

type lifecycleBoundaryFixture struct {
	*fixture
	session  store.Session
	children []fakeIssue
	parents  map[int]int
}

func newLifecycleBoundaryFixture(t *testing.T) *lifecycleBoundaryFixture {
	t.Helper()
	f := newFixture(t)
	x := &lifecycleBoundaryFixture{fixture: f, parents: map[int]int{}}
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		out, err := f.ranSuccessfully(ctx, req)
		out.ResearchGraph = true
		return out, err
	}
	x.restart()
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent Plan", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
	f.settle()
	x.session = f.sessionOf(testIssue)
	if _, err := f.db.AdmitPlanPath(t.Context(), x.session.ID); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared"}
	for i, key := range []string{"affected", "healthy"} {
		number, id := 200+i, int64(2000+i)
		draft := planbrief.PlanDraft{Key: key, Name: key, UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "review"}, Problem: "Unclear", InScope: []string{"draft"}, NotInScope: []string{}}
		title, body, err := report.RenderPlanDraft(testIssue, int64(i+1), draft, "story", planbrief.ContentScope{})
		if err != nil {
			t.Fatal(err)
		}
		body = fmt.Sprintf("<!-- lf:plan-story parent=1230 key=%s -->\n", key) + body
		binding, err := f.db.ReservePlanStory(t.Context(), x.session.TaskRef, key)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.db.BindPlanStoryIssue(t.Context(), x.session.TaskRef, key, id, number); err != nil {
			t.Fatal(err)
		}
		request := "create-" + key
		if err = f.db.PutPlanStoryCandidate(t.Context(), store.PlanStoryCandidate{Parent: x.session.TaskRef, Key: key, RequestID: request, Candidate: "candidate-" + key, RequestJSON: `{"operation":"create"}`}); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		binding.IssueID, binding.IssueNumber, binding.Revision = id, number, turn.PlanIssueRevision(title, body)
		binding.Candidate, binding.DraftJSON = "candidate-"+key, string(raw)
		if err = f.db.ApplyPlanStoryDraft(t.Context(), binding, "", store.PlanStoryEffect{Parent: x.session.TaskRef, RequestID: request, Phase: "attach", Outcome: "applied", IssueID: id, IssueNumber: number, Revision: binding.Revision}); err != nil {
			t.Fatal(err)
		}
		if err = state.PutCandidate(draft, "story", binding.Candidate); err != nil {
			t.Fatal(err)
		}
		ref, err := state.ReviewRevision(key, "story")
		if err != nil {
			t.Fatal(err)
		}
		state.Stories[i].Approvals = map[string]string{"story": ref}
		child := fakeIssue{ID: id, Number: number, Title: title, Body: body, State: "open", User: stranger()}
		x.children = append(x.children, child)
		f.forge.addIssue(child)
	}
	state.AssembledApproval = "prior-reviewed-set"
	if _, err := f.db.SavePlanWorkflowState(t.Context(), x.session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	return x
}

func (x *lifecycleBoundaryFixture) restart() {
	x.daemon = x.newDaemon(daemon.Options{Forge: lifecycleBoundaryForge{Client: x.client, parents: x.parents}})
}
func (x *lifecycleBoundaryFixture) state() planbrief.PlanState {
	x.t.Helper()
	state, _, err := x.db.GetPlanWorkflowState(x.t.Context(), x.session.TaskRef)
	if err != nil {
		x.t.Fatal(err)
	}
	return state
}
func (x *lifecycleBoundaryFixture) assertNoChildMutation() {
	x.t.Helper()
	x.forge.mu.Lock()
	count := x.forge.issueMutations
	x.forge.mu.Unlock()
	if count != 0 {
		x.t.Fatalf("discovery attempted %d child mutations", count)
	}
}
func (x *lifecycleBoundaryFixture) assertHealthyReply(id int64) {
	x.t.Helper()
	x.forge.addComment(201, id, owner(), "continue healthy story")
	x.settle()
	calls := x.turns.calls()
	if !slices.ContainsFunc(calls, func(req turn.Request) bool {
		return req.Issue.Number == testIssue && req.Trigger.PlanReply != nil && req.Trigger.PlanReply.Key == "healthy" && req.Trigger.CommentID == id
	}) {
		x.t.Fatalf("healthy reply did not reach parent executor: %+v logs=%s", calls, x.logs)
	}
	x.assertNoChildMutation()
}

func TestPlanLifecycleBoundary_ClosedReasonsPreserveSiblingProgress(t *testing.T) {
	for _, reason := range []string{"completed", "not_planned", ""} {
		t.Run("reason-"+reason, func(t *testing.T) {
			x := newLifecycleBoundaryFixture(t)
			prior := x.state()
			child := x.children[0]
			child.State, child.StateReason = "closed", reason
			x.forge.addIssue(child)
			x.assertHealthyReply(60)
			state := x.state()
			affected, healthy := state.Stories[0], state.Stories[1]
			if affected.Lifecycle.State != "closed" || affected.Lifecycle.Reason != reason || affected.Retired || affected.Active() || len(affected.Approvals) != 0 {
				t.Fatalf("closure conflated with retirement or approval: %+v", affected)
			}
			if healthy.Approvals["story"] != prior.Stories[1].Approvals["story"] || state.AssembledApproval != "" {
				t.Fatalf("approval invalidation crossed sibling boundary: %+v", state)
			}
			binding, err := x.db.GetPlanStoryDraft(t.Context(), x.session.TaskRef, "affected")
			if err != nil || binding.Retired || binding.IssueID != child.ID || binding.Revision != turn.PlanIssueRevision(child.Title, child.Body) {
				t.Fatalf("closure destroyed binding: %+v %v", binding, err)
			}
			history, err := x.db.PlanWorkflowHistory(t.Context(), x.session.TaskRef, 1)
			if err != nil || history.Stories[0].Lifecycle.State != "" || len(history.Stories[0].Approvals) == 0 {
				t.Fatalf("history not preserved: %+v %v", history, err)
			}
			x.restart()
			x.settle()
			if len(x.turns.calls()) != 1 || x.state().Stories[0].Lifecycle.Reason != reason {
				t.Fatal("restart lost closure or replayed sibling input")
			}
			x.assertNoChildMutation()
		})
	}
}

func TestPlanLifecycleBoundary_LocalIdentityHoldsAllowSibling(t *testing.T) {
	for _, kind := range []string{"missing", "identity", "reparent", "parent-missing"} {
		t.Run(kind, func(t *testing.T) {
			x := newLifecycleBoundaryFixture(t)
			child := x.children[0]
			switch kind {
			case "missing":
				x.forge.mu.Lock()
				rs := x.forge.state(testOwnerName, testRepoName)
				rs.issues = slices.DeleteFunc(rs.issues, func(issue fakeIssue) bool { return issue.Number == 200 })
				x.forge.mu.Unlock()
			case "identity":
				child.ID = 9999
				child.State = "closed"
				x.forge.addIssue(child)
			case "reparent":
				x.parents[200] = 321
				x.forge.addIssue(fakeIssue{ID: 3210, Number: 321, Title: "Other parent", State: "closed", User: stranger()})
			case "parent-missing":
				x.parents[200] = 321
			}
			x.assertHealthyReply(60)
			affected := x.state().Stories[0]
			if affected.LifecycleHold == "" || affected.Lifecycle.State != "" || affected.Retired || !affected.Blocked() {
				t.Fatalf("invalid binding inferred lifecycle instead of hold: %+v", affected)
			}
			binding, err := x.db.GetPlanStoryDraft(t.Context(), x.session.TaskRef, "affected")
			if err != nil || binding.IssueID != 2000 || binding.IssueNumber != 200 {
				t.Fatalf("invalid binding rebound: %+v %v", binding, err)
			}
			state := x.state()
			if state.PlanReady(planbrief.ContentScope{}) {
				t.Fatal("identity hold permitted readiness")
			}
		})
	}
}

func TestPlanLifecycleBoundary_ReopenReobservesWithoutApproval(t *testing.T) {
	x := newLifecycleBoundaryFixture(t)
	child := x.children[0]
	child.State, child.StateReason = "closed", "completed"
	x.forge.addIssue(child)
	x.settle()
	if x.state().Stories[0].Lifecycle.State != "closed" {
		t.Fatalf("closure not observed: %+v logs=%s", x.state(), x.logs)
	}
	x.restart()
	child.State, child.StateReason = "open", "reopened"
	x.forge.addIssue(child)
	x.settle()
	affected := x.state().Stories[0]
	if !affected.Active() || !affected.Lifecycle.Reopened || len(affected.Approvals) != 0 || affected.Wait != nil || affected.Stage != "story" {
		t.Fatalf("reopen resurrected stale approval or wait: %+v", affected)
	}
	state := x.state()
	if !slices.Contains(state.EligiblePlanStories(), "affected") {
		t.Fatalf("same valid child not reconsidered: %+v", x.state())
	}
	x.assertNoChildMutation()
}

func TestPlanLifecycleBoundary_FrozenGenerationIsNotReopened(t *testing.T) {
	x := newLifecycleBoundaryFixture(t)
	child := x.children[0]
	child.State = "closed"
	x.forge.addIssue(child)
	x.settle()
	if err := x.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: x.session.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := x.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: x.session.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	x.restart()
	child.State = "open"
	child.Body = "changed content that must not replace frozen scope"
	x.forge.addIssue(child)
	x.settle()
	feature, err := x.db.GetManagedFeature(t.Context(), x.session.TaskRef)
	if err != nil || feature.State != "implementing" {
		t.Fatalf("frozen generation changed: %+v %v", feature, err)
	}
	binding, err := x.db.GetPlanStoryDraft(t.Context(), x.session.TaskRef, "affected")
	if err != nil || binding.Revision != turn.PlanIssueRevision(x.children[0].Title, x.children[0].Body) {
		t.Fatalf("frozen draft was revised: %+v %v", binding, err)
	}
	if !x.state().Stories[0].Lifecycle.Reopened || len(x.turns.calls()) != 0 {
		t.Fatalf("frozen reopen not retained observationally: %+v calls=%+v", x.state(), x.turns.calls())
	}
	x.assertNoChildMutation()
}

func TestPlanLifecycleBoundary_ReopenChangedRevisionIsHeld(t *testing.T) {
	x := newLifecycleBoundaryFixture(t)
	child := x.children[0]
	child.State = "closed"
	x.forge.addIssue(child)
	x.settle()
	x.restart()
	child.State = "open"
	child.Body = "Content without the required bound story provenance"
	x.forge.addIssue(child)
	x.assertHealthyReply(60)
	state := x.state()
	affected := state.Stories[0]
	if !affected.Lifecycle.Reopened || affected.LifecycleHold == "" || len(affected.Approvals) != 0 || slices.Contains(state.EligiblePlanStories(), "affected") {
		t.Fatalf("changed reopened revision was admitted without reevaluation: %+v", affected)
	}
	binding, err := x.db.GetPlanStoryDraft(t.Context(), x.session.TaskRef, "affected")
	if err != nil || binding.Revision != turn.PlanIssueRevision(x.children[0].Title, x.children[0].Body) {
		t.Fatalf("invalid reopened content replaced bound draft: %+v %v", binding, err)
	}
}

func TestPlanLifecycleBoundary_ClosureInvalidatesDependentAndRetainsEdge(t *testing.T) {
	x := newLifecycleBoundaryFixture(t)
	state, revision, err := x.db.GetPlanWorkflowState(t.Context(), x.session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	dependent := state.Stories[1]
	dependent.Key, dependent.Draft.Key = "dependent", "dependent"
	dependent.Draft.Dependencies.StoryKeys = []string{"affected"}
	dependent.Candidate = "candidate-dependent"
	state.Stories = append(state.Stories, dependent)
	if _, err := x.db.SavePlanWorkflowState(t.Context(), x.session.TaskRef, revision, state); err != nil {
		t.Fatal(err)
	}
	child := x.children[0]
	child.State, child.StateReason = "closed", "completed"
	x.forge.addIssue(child)
	x.assertHealthyReply(60)
	state = x.state()
	if !slices.Equal(state.Stories[2].Draft.Dependencies.StoryKeys, []string{"affected"}) || len(state.Stories[2].Approvals) != 0 || slices.Contains(state.EligiblePlanStories(), "dependent") || state.PlanReady(planbrief.ContentScope{}) {
		t.Fatalf("closure lost or silently satisfied dependency: %+v", state)
	}
}

package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Only the relationship is replaced; all discovery, pagination, owner reads,
// posts and reporting still go through the HTTP client and real SQLite.
type refinementForge struct {
	*github.Client
	parent int
}

func (f refinementForge) GetParentIssue(ctx context.Context, repo github.Repo, _ int) (github.Issue, error) {
	return f.GetIssue(ctx, repo, f.parent)
}

func managedRefinementFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger()})
	f.settle()
	s := f.sessionOf(testIssue)
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: s.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: s.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 200, IssueID: 2000, BodyHash: "hash", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	f.forge.addIssue(fakeIssue{ID: 2000, Number: 200, Title: "Story", Body: "Current acceptance criteria", State: "closed", User: stranger()})
	return f
}

func TestStoryRefinementUsesParentAndIndependentDelivery(t *testing.T) {
	f := managedRefinementFixture(t)
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	renamed := owner()
	renamed.Login = "renamed-owner"
	f.forge.addComment(200, 50, stranger(), "do not admit this")
	f.forge.addComment(200, 60, renamed, "refine acceptance criteria")
	f.forge.addComment(testIssue, 70, owner(), "/implement go")
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Issue.Number != testIssue || calls[0].Mode != agent.ModePlan || calls[0].Trigger.CommentID != 60 {
		t.Fatalf("parent planning call = %+v\n%s", calls, f.logs)
	}
	assertRetentionLabels(t, f, "Mode: Plan")
	if calls[0].ToolProfile != "plan" {
		t.Fatalf("child effective permissions: %+v", calls[0])
	}
	if f.watermark() != 0 {
		t.Fatalf("child delivery advanced parent watermark to %d", f.watermark())
	}
	if _, err := f.db.GetSessionByTaskRef(t.Context(), store.TaskRef(store.ForgeGitHub, testOwnerName, testRepoName, 200)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child session exists: %v", err)
	}
	r, err := f.db.GetStoryRefinement(t.Context(), f.sessionOf(testIssue).ID, 60)
	if err != nil || !r.Consumed {
		t.Fatalf("refinement delivery: %+v %v", r, err)
	}
	f.forge.mu.Lock()
	defer f.forge.mu.Unlock()
	if len(f.forge.postedIssues) != 2 || f.forge.postedIssues[0] != 200 || !strings.Contains(f.forge.postedBodies[0], "issues/123") || !strings.Contains(f.forge.postedBodies[0], "issuecomment-60") {
		t.Fatalf("child report lacks provenance: %v %v", f.forge.postedIssues, f.forge.postedBodies)
	}
}

func TestStoryRefinementFailedPostRemainsPending(t *testing.T) {
	f := managedRefinementFixture(t)
	f.forge.addComment(200, 60, owner(), "refine acceptance criteria")
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, "unavailable", http.StatusBadGateway)
		return true
	}
	f.settle()
	pending, err := f.db.ListPendingStoryRefinements(t.Context(), f.sessionOf(testIssue).ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	if f.watermark() != 0 {
		t.Fatal("parent watermark advanced")
	}
	f.forge.onPostComment = nil
	f.settle()
	pending, err = f.db.ListPendingStoryRefinements(t.Context(), f.sessionOf(testIssue).ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after retry = %v, %v\n%s", pending, err, f.logs)
	}
}

func TestStoryRefinementLocksAfterPromotion(t *testing.T) {
	f := managedRefinementFixture(t)
	parent := f.sessionOf(testIssue)
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(200, 60, owner(), "change acceptance criteria")
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatal("locked story ran")
	}
	bodies, _, _ := f.forge.snapshot()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "locked after promotion") {
		t.Fatalf("missing lock notice: %v\n%s", bodies, f.logs)
	}
	if f.watermark() != 0 {
		t.Fatal("lock refusal advanced parent watermark")
	}
}

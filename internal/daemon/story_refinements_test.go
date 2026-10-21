package daemon_test

import (
	"context"
	"errors"
	"fmt"
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
	prepared, err := f.db.GetStoryRefinementACK(t.Context(), f.sessionOf(testIssue).ID, 60)
	if err != nil || !prepared.DispatchAttempted || prepared.PreparedBody == "" {
		t.Fatalf("uncertain acknowledgement intent = %+v, %v", prepared, err)
	}
	// A failed write response does not establish remote non-delivery. An empty
	// reconciliation observation cannot restore the burned dispatch right.
	f.forge.onPostComment = nil
	f.settle()
	pending, err = f.db.ListPendingStoryRefinements(t.Context(), f.sessionOf(testIssue).ID)
	if err != nil || len(pending) != 1 || pending[0].Consumed {
		t.Fatalf("uncertain refinement after retry = %v, %v\n%s", pending, err, f.logs)
	}
	retained, err := f.db.GetStoryRefinementACK(t.Context(), f.sessionOf(testIssue).ID, 60)
	if err != nil || retained.PreparedBody != prepared.PreparedBody || !retained.DispatchAttempted {
		t.Fatalf("acknowledgement intent changed on retry = %+v, %v", retained, err)
	}
	bodies, _, _ := f.forge.snapshot()
	if len(bodies) != 0 || len(f.turns.calls()) != 0 || f.watermark() != 0 {
		t.Fatalf("uncertain acknowledgement authorized duplicate publication or source advance: posts=%d turns=%d watermark=%d", len(bodies), len(f.turns.calls()), f.watermark())
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

// TestClosedManagedParentStopsCostingAReadEveryTick is the regression for the
// registry's missing terminal state.
//
// [store.DB.ListManagedFeatures] is the one discovery axis nothing gates on a
// forge listing — deliberately, because GitHub does not move a parent's
// timestamp when a child moves. Without a terminal state that made every closed
// parent a standing cost: one issue read per tick, forever, purely so
// materialise could rediscover the closure and drop the candidate again. With
// eighteen registered features and a sixty-second tick that was most of the
// daemon's hourly background allowance spent on nothing.
//
// The assertion is the shape rather than a number: the closure is observed
// once, and no later tick pays for it again.
func TestClosedManagedParentStopsCostingAReadEveryTick(t *testing.T) {
	f := managedRefinementFixture(t)
	f.forge.closeIssue(testIssue)
	f.settle()

	observed := f.forge.issueReadsOf(testIssue)
	if observed == 0 {
		t.Fatalf("the closure was never read, so nothing established it\n%s", f.logs)
	}
	for tick := range 3 {
		f.settle()
		if got := f.forge.issueReadsOf(testIssue); got != observed {
			t.Fatalf("tick %d re-read the closed parent: %d reads, want the %d already spent\n%s",
				tick+1, got, observed, f.logs)
		}
	}

	// Reopening is found through ordinary discovery: it moves the issue's
	// timestamp, so the listing returns it and [Daemon.sight] restores the
	// registry entry. Retirement must not be a one-way door.
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger()})
	f.settle()
	features, err := f.db.ListManagedFeatures(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 1 {
		t.Fatalf("the reopened parent is still retired: %+v\n%s", features, f.logs)
	}
}

// TestQuietStoriesCostNothingPerTick is the regression for the sweep that
// scaled with the registry instead of with the work.
//
// Story discovery used to read every registered child of every dispatched
// parent on every tick — the child, its parent link, its comments — whether or
// not anything had been said on any of them. At sixty ticks an hour that was
// the bulk of the daemon's background quota, spent to re-establish that nothing
// had happened.
//
// Two assertions, and both are the shape rather than a number: a quiet child
// costs nothing after its first sweep, and a child that is commented on is
// still found. The second is what stops the first being satisfied by simply
// never looking.
func TestQuietStoriesCostNothingPerTick(t *testing.T) {
	f := managedRefinementFixture(t)
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
	f.settle()

	// The first sweep is owed: nothing has ever read this child's history, so
	// no listing can vouch for it. It is what leaves the cursor behind.
	settled := f.forge.issueReadsOf(200)
	if settled == 0 {
		t.Fatalf("the child was never swept, so no cursor was established\n%s", f.logs)
	}
	for tick := range 3 {
		f.settle()
		if got := f.forge.issueReadsOf(200); got != settled {
			t.Fatalf("tick %d re-read a child nobody had commented on: %d reads, want the %d already spent\n%s",
				tick+1, got, settled, f.logs)
		}
	}

	// The repository comment listing names the child, which is what owes it the
	// next sweep — the parent's own timestamp never moved.
	f.forge.addComment(200, 90, owner(), "refine acceptance criteria")
	f.settle()
	if got := f.forge.issueReadsOf(200); got <= settled {
		t.Fatalf("a commented child was not swept: still %d reads\n%s", got, f.logs)
	}
	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Trigger.CommentID != 90 {
		t.Fatalf("the child's comment did not reach the parent: %+v\n%s", calls, f.logs)
	}
}

// conflictingStoryForge refuses the parent link of one story the way a retained
// ticket identity check does; every other read goes through the HTTP client.
type conflictingStoryForge struct {
	refinementForge
	failing int
}

func (f conflictingStoryForge) GetParentIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	if number == f.failing {
		return github.Issue{}, fmt.Errorf("read story parent: %w", store.ErrTicketIdentityConflict)
	}
	return f.refinementForge.GetParentIssue(ctx, repo, number)
}

// TestFailingStorySweepDoesNotHoldParentComments is the regression for #1010:
// one story whose sweep hits a retained identity conflict failed every parent
// turn, so owner comments on the parent were never admitted.
func TestFailingStorySweepDoesNotHoldParentComments(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{Forge: conflictingStoryForge{refinementForge: refinementForge{Client: f.client, parent: testIssue}, failing: 200}})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
	f.settle()
	s := f.sessionOf(testIssue)
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: s.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	for _, story := range []store.ManagedStory{
		{ParentTask: s.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 200, IssueID: 2000, BodyHash: "hash", State: "planning"},
		{ParentTask: s.TaskRef, Generation: 1, Key: "US-2", IssueNumber: 201, IssueID: 2010, BodyHash: "hash", State: "planning"},
	} {
		if err := f.db.PutManagedStory(t.Context(), story); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: s.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	f.forge.addIssue(fakeIssue{ID: 2000, Number: 200, Title: "Story 1", Body: "criteria", User: stranger()})
	f.forge.addIssue(fakeIssue{ID: 2010, Number: 201, Title: "Story 2", Body: "criteria", User: stranger()})
	f.forge.addComment(200, 50, stranger(), "worker progress on story 1")
	f.forge.addComment(201, 60, stranger(), "worker progress on story 2")
	f.forge.addComment(testIssue, 70, owner(), "resume the parked story")
	f.settle()

	// C1: the parent's own comment is admitted in the same turn.
	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Issue.Number != testIssue || calls[0].Trigger.CommentID != 70 {
		t.Fatalf("parent comment not admitted: %+v\n%s", calls, f.logs)
	}
	if f.watermark() != 70 {
		t.Fatalf("parent watermark = %d, want 70\n%s", f.watermark(), f.logs)
	}
	logs := f.logs.String()
	if strings.Contains(logs, "could not refine managed stories") {
		t.Fatalf("a story-specific failure still failed the parent turn\n%s", logs)
	}
	// C4: the skipped story is named at warning level with the error.
	var warned bool
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, "WARN") && strings.Contains(line, "could not sweep a managed story") &&
			strings.Contains(line, "200") && strings.Contains(line, store.ErrTicketIdentityConflict.Error()) {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("no warning names the skipped story and its error\n%s", logs)
	}

	// C2: the failing story stays owed with an unmoved cursor; the other is swept.
	awaiting, err := f.db.ListStoriesAwaitingDiscovery(t.Context(), s.ID, s.TaskRef, 1)
	if err != nil || len(awaiting) != 1 || awaiting[0] != 200 {
		t.Fatalf("stories awaiting discovery = %v, %v", awaiting, err)
	}
	if cursor, err := f.db.StoryDiscoveryCursor(t.Context(), s.ID, 200); err != nil || cursor != 0 {
		t.Fatalf("failing story cursor = %d, %v", cursor, err)
	}
	if cursor, err := f.db.StoryDiscoveryCursor(t.Context(), s.ID, 201); err != nil || cursor != 60 {
		t.Fatalf("swept story cursor = %d, %v", cursor, err)
	}
}

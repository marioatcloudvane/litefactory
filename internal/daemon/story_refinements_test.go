package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
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

// handledStoryFixture is a managed parent whose story #200 has its own session,
// and that session already dealt with a plain owner comment (60) and a refused
// approval (61) during Plan: its watermark is at 61. Neither is a consumed
// Plan reply. The story is then owed a parent sweep from cursor 0 (#1157).
func handledStoryFixture(t *testing.T, state string) (*fixture, store.Session) {
	t.Helper()
	f := managedRefinementFixture(t)
	parent := f.sessionOf(testIssue)
	if state != "planning" {
		if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: state}, "planning"); err != nil {
			t.Fatal(err)
		}
	}
	f.forge.addComment(200, 60, owner(), "what does this improve? I cannot tell from the story")
	f.forge.addComment(200, 61, owner(), "/approve-plan story-999 a stage that is not open")
	story, err := f.db.GetOrCreateSession(t.Context(), f.repo, 200)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.BootstrapWatermark(t.Context(), story.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdvanceWatermark(t.Context(), story.ID, 61); err != nil {
		t.Fatal(err)
	}
	if err = f.db.MarkStoryDiscoveryPending(t.Context(), parent.ID, 200); err != nil {
		t.Fatal(err)
	}
	return f, parent
}

// assertNotHandledByParent checks that the parent neither admitted, answered
// nor ran the given story comments, and that its cursor moved past them.
func assertNotHandledByParent(t *testing.T, f *fixture, parent store.Session, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if r, err := f.db.GetStoryRefinement(t.Context(), parent.ID, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("comment %d admitted as a story refinement: %+v %v\n%s", id, r, err, f.logs)
		}
		for _, call := range f.turns.calls() {
			if call.Trigger.CommentID == id {
				t.Fatalf("comment %d ran on the parent: %+v", id, call)
			}
		}
		bodies, reacted, _ := f.forge.snapshot()
		for _, body := range bodies {
			if strings.Contains(body, fmt.Sprintf("issuecomment-%d", id)) || strings.Contains(body, "locked after promotion") {
				t.Fatalf("comment %d answered by the parent: %q", id, body)
			}
		}
		for _, r := range reacted {
			if r == id {
				t.Fatalf("comment %d acknowledged by the parent: %v", id, reacted)
			}
		}
		cursor, err := f.db.StoryDiscoveryCursor(t.Context(), parent.ID, 200)
		if err != nil || cursor < id {
			t.Fatalf("parent cursor %d not past comment %d: %v", cursor, id, err)
		}
	}
}

// C4 without project connections: comments the story's own session handled
// get no lock reply after promotion.
func TestStoryRefinementSkipsCommentsTheStorySessionHandled(t *testing.T) {
	f, parent := handledStoryFixture(t, "implementing")
	f.settle()
	assertNotHandledByParent(t, f, parent, 60, 61)
	if len(f.turns.calls()) != 0 {
		t.Fatalf("locked story ran: %+v", f.turns.calls())
	}
}

// C2: a genuinely new comment above the story session's watermark still gets
// the lock notice exactly once.
func TestStoryRefinementLocksNewCommentAboveStoryWatermark(t *testing.T) {
	f, parent := handledStoryFixture(t, "implementing")
	f.forge.addComment(200, 90, owner(), "change acceptance criteria")
	f.settle()
	f.settle()
	if r, err := f.db.GetStoryRefinement(t.Context(), parent.ID, 90); err != nil || !r.Consumed {
		t.Fatalf("new comment not admitted and answered: %+v %v\n%s", r, err, f.logs)
	}
	bodies, _, _ := f.forge.snapshot()
	locks := 0
	for _, body := range bodies {
		if strings.Contains(body, "locked after promotion") {
			locks++
		}
	}
	if locks != 1 || len(bodies) != 1 {
		t.Fatalf("lock notices = %d of %d posts, want exactly one: %v", locks, len(bodies), bodies)
	}
	for _, id := range []int64{60, 61} {
		if _, err := f.db.GetStoryRefinement(t.Context(), parent.ID, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("handled comment %d admitted: %v", id, err)
		}
	}
}

// C1: a reply the parent's Plan state already consumed is not answered again,
// even when no story-session watermark covers it.
func TestStoryRefinementSkipsConsumedPlanReplies(t *testing.T) {
	f := managedRefinementFixture(t)
	parent := f.sessionOf(testIssue)
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(200, 60, owner(), "/approve-plan story-001 review")
	if _, err := f.db.SavePlanWorkflowState(t.Context(), parent.TaskRef, 0, planbrief.PlanState{SharedRevision: "shared", ConsumedReplies: []int64{60}}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.MarkStoryDiscoveryPending(t.Context(), parent.ID, 200); err != nil {
		t.Fatal(err)
	}
	f.settle()
	assertNotHandledByParent(t, f, parent, 60)
}

// C5: while still planning, a comment the story session already handled is
// neither admitted nor run on the parent.
func TestStoryRefinementPlanningSkipsCommentsTheStorySessionHandled(t *testing.T) {
	f, parent := handledStoryFixture(t, "planning")
	f.settle()
	assertNotHandledByParent(t, f, parent, 60, 61)
	if len(f.turns.calls()) != 0 {
		t.Fatalf("handled comment ran on the parent: %+v", f.turns.calls())
	}
}

// C4 with a project connection: the story's own session is found by ticket
// identity, and the comments it handled get no lock reply after promotion.
func TestStoryRefinementSkipsCommentsTheStorySessionHandledWithConnection(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	original := w.forge.server.Config.Handler
	w.forge.server.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		base := "/repos/" + w.repo.Slug()
		if strings.HasPrefix(r.URL.Path, base+"/issues/") && strings.HasSuffix(r.URL.Path, "/parent") {
			r = r.Clone(r.Context())
			r.URL.Path = base + "/issues/" + strconv.Itoa(testIssue)
		}
		original.ServeHTTP(rw, r)
	})
	origin := recoveryProvider(t, w.fixture)
	source := configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "story-session-connection", false)
	f := w.fixture
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 10000 + testIssue, Number: testIssue, Title: "Parent feature", User: stranger()})
	f.clock.advance(scheduleInterval)
	f.settle()
	parent := f.sessionOf(testIssue)
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: parent.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 200, IssueID: 10200, BodyHash: "hash", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	f.forge.addIssue(fakeIssue{ID: 10200, Number: 200, Title: "Story", Body: "Current acceptance criteria", State: "closed", User: stranger()})
	f.forge.addComment(200, 60, owner(), "what does this improve? I cannot tell from the story")
	f.forge.addComment(200, 61, owner(), "/approve-plan story-999 a stage that is not open")
	ref := ticket.Ref{Identity: ticket.Identity{Provider: source.Provider, Origin: origin, RemoteID: "10200"}, Locator: ticket.Locator{Provider: source.Provider, Origin: origin, Container: source.Destination, Key: "200"}}
	story, err := f.db.AdmitManagedChild(t.Context(), parent.ID, ref, "github:"+w.repo.Slug()+"#200")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.BootstrapTicketWatermark(t.Context(), story.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdvanceWatermark(t.Context(), story.ID, 61); err != nil {
		t.Fatal(err)
	}
	if err = f.db.MarkStoryDiscoveryPending(t.Context(), parent.ID, 200); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(scheduleInterval)
	f.settle()
	if cursor, err := f.db.StoryDiscoveryCursor(t.Context(), parent.ID, 200); err != nil || cursor < 61 {
		t.Fatalf("the parent never swept the story: cursor=%d %v\n%s", cursor, err, f.logs)
	}
	assertNotHandledByParent(t, f, parent, 60, 61)
}

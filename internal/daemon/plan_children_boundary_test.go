package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
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

// planChildFixture seeds a parent Plan with one child story waiting for review
// and returns it before any owner comment exists.
func planChildFixture(t *testing.T, fn func(context.Context, *fixture, turn.Request) (turn.Result, error)) (*fixture, store.Session) {
	t.Helper()
	f := newFixture(t)
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		return fn(ctx, f, req)
	}
	f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent Plan", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
	f.settle()
	session := f.sessionOf(testIssue)
	if _, err := f.db.AdmitPlanWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
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
	return f, session
}

func planChildrenRoutingCase(t *testing.T, ownerBody string, locked bool) {
	t.Helper()
	attempts := 0
	f, session := planChildFixture(t, func(ctx context.Context, f *fixture, req turn.Request) (turn.Result, error) {
		attempts++
		if locked && attempts == 1 {
			return turn.Result{Disposition: turn.DispositionLocked}, nil
		}
		out, err := f.ranSuccessfully(ctx, req)
		out.ResearchGraph = true
		return out, err
	})
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

// A child reply the review rules refuse is answered once on the sub-issue and
// consumed, a restart posts nothing more, and a later reply still reaches the
// Plan turn instead of stalling behind the refused one.
func TestPlanChildrenBoundary_RefusedReplyIsAnsweredOnceAndQueueProgresses(t *testing.T) {
	const refusedText = "could not be applied to the current review"
	f, session := planChildFixture(t, func(ctx context.Context, f *fixture, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == 60 {
			return turn.Result{}, &turn.PlanReplyRefusal{Reason: "This reply " + refusedText + " of this story.", Err: errors.New("stale")}
		}
		out, err := f.ranSuccessfully(ctx, req)
		out.ResearchGraph = true
		return out, err
	})
	refusals := func() int {
		posted, _, _ := f.forge.snapshot()
		n := 0
		for _, body := range posted {
			if strings.Contains(body, refusedText) {
				n++
			}
		}
		return n
	}
	f.forge.addComment(200, 60, owner(), "/approve-plan story story candidate")
	f.settle()
	if n := refusals(); n != 1 {
		t.Fatalf("refusal replies = %d, want 1\nlogs:\n%s", n, f.logs)
	}
	if cursor, err := f.db.StoryDiscoveryCursor(t.Context(), session.ID, 200); err != nil || cursor != 60 {
		t.Fatalf("refused reply not consumed: cursor=%d err=%v", cursor, err)
	}

	// A restart retries nothing and posts nothing more.
	f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
	f.settle()
	f.settle()
	if n := refusals(); n != 1 {
		t.Fatalf("refusal replies after restart = %d, want 1", n)
	}
	if got := len(f.turns.calls()); got != 1 {
		t.Fatalf("turn calls after restart = %d, want 1", got)
	}

	f.forge.addComment(200, 70, owner(), "Please narrow the scope")
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 2 || calls[1].Trigger.PlanReply == nil || calls[1].Trigger.PlanReply.ID != 70 {
		t.Fatalf("the reply after the refusal did not progress: %+v\nlogs:\n%s", calls, f.logs)
	}
	if n := refusals(); n != 1 {
		t.Fatalf("refusal replies after progress = %d, want 1", n)
	}
}

// countingPlanForge counts the per-issue reads a graph Plan parent's worker
// makes, so a test can say which children a tick touched.
type countingPlanForge struct {
	refinementForge
	mu       sync.Mutex
	issues   map[int]int
	parents  map[int]int
	comments map[int]int
}

func newCountingPlanForge(f *fixture) *countingPlanForge {
	return &countingPlanForge{refinementForge: refinementForge{Client: f.client, parent: testIssue}, issues: map[int]int{}, parents: map[int]int{}, comments: map[int]int{}}
}

func (c *countingPlanForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	c.mu.Lock()
	c.issues[number]++
	c.mu.Unlock()
	return c.Client.GetIssue(ctx, repo, number)
}

func (c *countingPlanForge) GetParentIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	c.mu.Lock()
	c.parents[number]++
	c.mu.Unlock()
	return c.Client.GetIssue(ctx, repo, c.parent)
}

func (c *countingPlanForge) ListCommentsAfter(ctx context.Context, repo github.Repo, number int, after int64) ([]github.Comment, error) {
	c.mu.Lock()
	c.comments[number]++
	c.mu.Unlock()
	return c.Client.ListCommentsAfter(ctx, repo, number, after)
}

// childReads is every child-scoped read so far: issue, parent link, comments.
func (c *countingPlanForge) childReads(number int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.issues[number] + c.parents[number] + c.comments[number]
}

func (c *countingPlanForge) parentReads() (issue, comments int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.issues[testIssue], c.comments[testIssue]
}

func countingPlanChildFixture(t *testing.T) (*fixture, store.Session, *countingPlanForge) {
	t.Helper()
	f, session := planChildFixture(t, func(ctx context.Context, f *fixture, req turn.Request) (turn.Result, error) {
		out, err := f.ranSuccessfully(ctx, req)
		out.ResearchGraph = true
		return out, err
	})
	forge := newCountingPlanForge(f)
	f.daemon = f.newDaemon(daemon.Options{Forge: forge})
	return f, session, forge
}

// A never-swept child is swept once without any listing signal, and after
// that an idle graph Plan parent reads none of its children.
func TestPlanChildrenSweep_IdleParentReadsNoChildren(t *testing.T) {
	f, session, forge := countingPlanChildFixture(t)
	f.settle()
	// The child is also its own task (first sight lists its comments), so only
	// the parent sweep's issue and parent-link reads are counted exactly.
	if forge.issues[200] != 1 || forge.parents[200] != 1 || forge.comments[200] < 1 {
		t.Fatalf("first sweep reads issue=%d parent=%d comments=%d, want 1, 1, >=1\n%s", forge.issues[200], forge.parents[200], forge.comments[200], f.logs)
	}
	if owed, err := f.db.StoryDiscoveryOwed(t.Context(), session.ID, 200); err != nil || owed {
		t.Fatalf("a completed first sweep left the child owed: %v %v", owed, err)
	}
	// The issue listing re-lists a just-created child for its overlap window,
	// and each listing names it again. Past the window, nothing names it.
	f.clock.advance(daemon.ListingOverlap + time.Minute)
	f.settle()
	swept := forge.childReads(200)
	for tick := range 2 {
		issue, comments := forge.parentReads()
		f.settle()
		if got := forge.childReads(200); got != swept {
			t.Fatalf("idle tick %d read the child %d more times\n%s", tick+1, got-swept, f.logs)
		}
		gotIssue, gotComments := forge.parentReads()
		if gotIssue-issue != 1 || gotComments-comments > 1 {
			t.Fatalf("idle tick %d parent reads issue=%d comments=%d, want 1 and at most 1", tick+1, gotIssue-issue, gotComments-comments)
		}
	}
}

// A child named by the comment listing is swept on the next parent worker, its
// owner reply is routed as before, and once a sweep completes the child is not
// read again until it is named again.
func TestPlanChildrenSweep_NamedChildIsSweptThenQuiet(t *testing.T) {
	f, session, forge := countingPlanChildFixture(t)
	f.settle()
	f.settle()
	quiet := forge.childReads(200)

	f.forge.addComment(200, 60, owner(), "/approve-plan story story candidate")
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Trigger.PlanReply == nil || calls[0].Trigger.PlanReply.ID != 60 {
		t.Fatalf("named child's reply not routed: %+v\n%s", calls, f.logs)
	}
	if forge.comments[200] <= 1 || forge.childReads(200) <= quiet {
		t.Fatalf("named child was not swept")
	}
	// The reply ended the worker early, so the debt stands. The comment listing
	// keeps naming the child for its overlap window; past it, one more sweep
	// finds nothing new and retires the debt.
	f.clock.advance(daemon.ListingOverlap + time.Minute)
	f.settle()
	if owed, err := f.db.StoryDiscoveryOwed(t.Context(), session.ID, 200); err != nil || owed {
		t.Fatalf("child still owed after a complete sweep: %v %v", owed, err)
	}
	settled := forge.childReads(200)
	f.settle()
	if got := forge.childReads(200); got != settled {
		t.Fatalf("a child nobody named was read %d more times\n%s", got-settled, f.logs)
	}
	if got := len(f.turns.calls()); got != 1 {
		t.Fatalf("turn calls = %d, want 1", got)
	}
}

// A sweep whose explanation is not confirmed leaves the child owed, so the next
// tick reads it again and no owner comment is lost.
func TestPlanChildrenSweep_UnconfirmedExplanationKeepsTheDebt(t *testing.T) {
	f, session, forge := countingPlanChildFixture(t)
	f.settle()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, `{"message":"lost reply response"}`, http.StatusInternalServerError)
		return true
	}
	f.forge.addComment(200, 60, owner(), "/approve-plan story story old")
	f.settle()
	if owed, err := f.db.StoryDiscoveryOwed(t.Context(), session.ID, 200); err != nil || !owed {
		t.Fatalf("an unconfirmed explanation retired the child's debt: %v %v\n%s", owed, err, f.logs)
	}
	// Past the listing overlap nothing names the child again, so only the
	// retained debt can make the next tick read it.
	before := forge.parents[200]
	f.clock.advance(daemon.ListingOverlap + time.Minute)
	f.settle()
	if forge.parents[200] <= before {
		t.Fatalf("the still-owed child was not read again\n%s", f.logs)
	}
	if !strings.Contains(f.logs.String(), "Plan child reply awaits confirmed explanation") {
		t.Fatalf("the sweep never reached an unconfirmed explanation\n%s", f.logs)
	}
	if owed, err := f.db.StoryDiscoveryOwed(t.Context(), session.ID, 200); err != nil || !owed {
		t.Fatalf("an unconfirmed explanation retired the child's debt: %v %v", owed, err)
	}
	before = forge.parents[200]
	f.clock.advance(daemon.ListingOverlap + time.Minute)
	f.settle()
	if forge.parents[200] <= before {
		t.Fatalf("the child was not read again after an unconfirmed explanation\n%s", f.logs)
	}
}

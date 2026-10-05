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
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// planFailureTurn stands in for the graph Plan turn, and only for it. What the
// daemon, the reporter, the store and the forge do with its result is production
// code: the run, the graph instance and step attempt are real rows, and the
// failure comment is published by the real turn.Reporter.ReportGraphFailure to the
// fake forge over HTTP. A replay of a failed run completes the missing failure
// comment before returning, which is what turn.Execute does for a failed Plan run
// that is replayed.
type planFailureTurn struct {
	f *fixture
	// outcome is how the step ended: OutcomeFailed or OutcomeAbandoned (stopped).
	outcome harness.Outcome
	// conflict makes the run itself succeed while the graph's failure node
	// publishes the story-effect conflict.
	conflict bool
	cause    string
	// silent makes the turn return a failed run with no error and publish no
	// failure comment, on the first call and on replays: the daemon's own guard
	// is then the only thing between the owner's comment and being handled.
	silent bool

	mu    sync.Mutex
	calls int
	// reportErrs collects the publication errors each call met.
	reportErrs []error
}

func (p *planFailureTurn) execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	f := p.f
	if req.Mode != agent.ModePlan {
		return turn.Result{}, errors.New("test executor expects a Plan request")
	}
	session, err := f.db.GetSession(ctx, req.SessionID)
	if err != nil {
		return turn.Result{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return turn.Result{}, err
	}
	result := turn.Result{Disposition: turn.DispositionRan, SessionID: session.ID, TriggerCommentID: req.Trigger.CommentID, Ref: ref, Session: session, Outcome: p.outcome, ResearchGraph: true}

	existing, replay, err := f.db.GetRunByTrigger(ctx, session.ID, req.Trigger.CommentID)
	if err != nil {
		return turn.Result{}, err
	}
	if replay {
		result.Disposition = turn.DispositionAlreadyRan
		result.Run = existing
		if !existing.Succeeded() {
			// Replay of a failed Plan run: publish its failure comment if the
			// run never managed to.
			reported, rerr := f.db.GraphFailureReported(ctx, existing.ID, turn.PlanFailureNode)
			if rerr != nil {
				return result, rerr
			}
			if !reported && !p.silent {
				attempts, aerr := f.db.ListGraphNodeAttemptsByRun(ctx, existing.ID)
				if aerr != nil || len(attempts) == 0 {
					return result, errors.Join(aerr, errors.New("failed run has no attempt"))
				}
				if _, perr := f.reporter.ReportGraphFailure(ctx, req, turn.PlanWorkflowPolicy(), attempts[len(attempts)-1], turn.PlanFailureNode, errors.New(existing.Error)); perr != nil {
					p.noteReportErr(perr)
					return result, perr
				}
			}
		}
		return result, nil
	}

	sessionRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	if err != nil {
		return turn.Result{}, err
	}
	segment, err := f.db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, sessionRef, store.AgentClaude)
	if err != nil {
		return turn.Result{}, err
	}
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: string(agent.ModePlan), Agent: store.AgentClaude, TriggerCommentID: req.Trigger.CommentID, LogPath: func(ordinal int) string {
		path, _ := naming.RunLogPath(f.paths.Home(), ref, ordinal)
		return path
	}})
	if err != nil {
		return turn.Result{}, err
	}
	now := f.clock.Now()
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: strings.Repeat("b", 64), CreatedAt: now})
	if err != nil {
		return turn.Result{}, err
	}
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "story_agent", StartedAt: now, NextDueAt: now})
	if err != nil {
		return turn.Result{}, err
	}
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "story_agent", RunID: run.ID, StartedAt: now})
	if err != nil {
		return turn.Result{}, err
	}
	status, outcome := store.GraphFailed, ""
	if p.conflict {
		status, outcome = store.GraphSucceeded, "conflict"
	}
	if err = f.db.FinishGraphNodeAttempt(ctx, attempt.ID, status, outcome, now); err != nil {
		return turn.Result{}, err
	}
	attempt, err = f.db.GetGraphNodeAttempt(ctx, attempt.ID)
	if err != nil {
		return turn.Result{}, err
	}

	runStatus, exit := store.RunFailed, int64(1)
	if p.conflict {
		runStatus, exit = store.RunSucceeded, 0
	}
	finished, err := f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: runStatus, ExitCode: &exit, Error: p.cause})
	if err != nil {
		return turn.Result{}, err
	}
	result.Run = finished

	if p.silent {
		return result, nil
	}
	// The step's own publication attempt, as the graph makes it.
	if _, perr := f.reporter.ReportGraphFailure(ctx, req, turn.PlanWorkflowPolicy(), attempt, turn.PlanFailureNode, errors.New(p.cause)); perr != nil {
		p.noteReportErr(perr)
		if p.conflict {
			return result, perr
		}
		return result, errors.Join(errors.New(p.cause), perr)
	}
	if p.conflict {
		return result, nil
	}
	return result, errors.New(p.cause)
}

func (p *planFailureTurn) noteReportErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reportErrs = append(p.reportErrs, err)
}

func (p *planFailureTurn) turnCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

const planFailureOwnerComment = int64(40)

// planFailureFixture registers a graph Plan session on issue 123 and returns
// with the owner's triggering comment 40 posted but not yet processed.
func planFailureFixture(t *testing.T, p *planFailureTurn) *fixture {
	t.Helper()
	f := newFixture(t)
	p.f = f
	f.turns.fn = p.execute
	f.plantCursor()
	f.issueWithLabels("Shape stories", "Mode: Plan")
	f.settle()
	if _, err := f.db.AdmitPlanWorkflowPath(t.Context(), f.sessionOf(testIssue).ID, "graph"); err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(testIssue, planFailureOwnerComment, owner(), "/plan shape stories")
	return f
}

func failureCommentsOnForge(f *fixture) []string {
	posted, _, _ := f.forge.snapshot()
	var out []string
	for _, body := range posted {
		if strings.Contains(body, "Plan could not finish this step") {
			out = append(out, body)
		}
	}
	return out
}

func requireFailureText(t *testing.T, body, cause string) {
	t.Helper()
	for _, want := range []string{"Cause:", cause, "To retry, reply on this issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("failure comment lacks %q:\n%s", want, body)
		}
	}
}

// settleUntilHandled ticks (advancing the clock so any backoff elapses) until the
// owner's comment is handled or the budget is spent. Each tick is a real daemon
// tick through the real completion path.
func settleUntilHandled(f *fixture, id int64, ticks int) bool {
	f.t.Helper()
	for range ticks {
		f.clock.advance(time.Minute)
		f.settle()
		if f.watermark() >= id {
			return true
		}
	}
	return false
}

// A failing or stopped Plan step publishes a failure comment with the cause and
// the retry advice, and the owner's triggering comment is handled together with
// it: never before the comment exists, and not again once it does.
func TestPlanFailureDaemon_FailedOrStoppedStepPublishesAndOnlyThenHandlesOwnerComment(t *testing.T) {
	for _, tt := range []struct {
		name    string
		outcome harness.Outcome
		cause   string
	}{
		{"failed", harness.OutcomeFailed, "the provider refused the request"},
		{"stopped", harness.OutcomeAbandoned, "daemon stopped mid step"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &planFailureTurn{outcome: tt.outcome, cause: tt.cause}
			f := planFailureFixture(t, p)

			first := f.settle()
			if first.Dispatched != 1 || p.turnCalls() < 1 {
				t.Fatalf("the owner's comment did not reach the Plan turn: %+v calls=%d logs=%s", first, p.turnCalls(), f.logs)
			}
			bodies := failureCommentsOnForge(f)
			if len(bodies) != 1 {
				t.Fatalf("expected one failure comment after the failing step, got %d\nlogs=%s", len(bodies), f.logs)
			}
			requireFailureText(t, bodies[0], tt.cause)
			runs := f.runs()
			if len(runs) != 1 || runs[0].Status != store.RunFailed {
				t.Fatalf("run is not failed: %+v", runs)
			}
			reported, err := f.db.GraphFailureReported(t.Context(), runs[0].ID, turn.PlanFailureNode)
			if err != nil || !reported {
				t.Fatalf("failure comment not recorded as published: %v %v", reported, err)
			}
			if !settleUntilHandled(f, planFailureOwnerComment, 6) {
				t.Fatalf("owner comment never handled after the failure comment was published: watermark=%d logs=%s", f.watermark(), f.logs)
			}
			// Handled once, and the failure comment is not posted again.
			for range 2 {
				f.clock.advance(time.Minute)
				f.settle()
			}
			if got := len(failureCommentsOnForge(f)); got != 1 {
				t.Fatalf("failure comment posted %d times", got)
			}
			if got := p.turnCalls(); got > 3 {
				t.Fatalf("handled comment was dispatched again: %d turns", got)
			}
			// A restarted daemon does not reprocess the handled comment either.
			calls := p.turnCalls()
			f.daemon = f.newDaemon(daemon.Options{})
			f.clock.advance(time.Minute)
			f.settle()
			if p.turnCalls() != calls || len(failureCommentsOnForge(f)) != 1 {
				t.Fatalf("restart re-ran a handled comment: turns %d->%d", calls, p.turnCalls())
			}
		})
	}
}

// A conflicting story effect ends the graph at the failure node with the run
// itself finished: the comment is published in-graph, once, and the owner's
// comment is handled with it.
func TestPlanFailureDaemon_ConflictPublishesInGraphAndHandlesOwnerComment(t *testing.T) {
	p := &planFailureTurn{conflict: true, cause: "story story-001 effect on \"story_draft\" ended conflict: owner edited the story"}
	f := planFailureFixture(t, p)
	f.settle()
	bodies := failureCommentsOnForge(f)
	if len(bodies) != 1 {
		t.Fatalf("expected one conflict comment, got %d\nlogs=%s", len(bodies), f.logs)
	}
	requireFailureText(t, bodies[0], "conflict")
	if !settleUntilHandled(f, planFailureOwnerComment, 4) {
		t.Fatalf("owner comment not handled with the conflict comment: %d logs=%s", f.watermark(), f.logs)
	}
	if got := len(failureCommentsOnForge(f)); got != 1 || p.turnCalls() != 1 {
		t.Fatalf("conflict comment posted %d times over %d turns", got, p.turnCalls())
	}
}

// A run whose failure comment cannot be published leaves the owner's comment
// unhandled: the watermark never reaches it, however many ticks pass, and no
// failure comment exists. Once the forge accepts the comment, the same run is
// completed and the owner's comment is handled together with it.
func TestPlanFailureDaemon_UnpublishedFailureLeavesOwnerCommentUnhandled(t *testing.T) {
	p := &planFailureTurn{outcome: harness.OutcomeFailed, cause: "the provider refused the request"}
	f := planFailureFixture(t, p)
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}
	for range 4 {
		f.clock.advance(time.Minute)
		f.settle()
		if got := f.watermark(); got >= planFailureOwnerComment {
			t.Fatalf("owner comment handled without a published failure comment: watermark=%d logs=%s", got, f.logs)
		}
	}
	if got := len(failureCommentsOnForge(f)); got != 0 {
		t.Fatalf("failure comment reached the forge although every post was refused: %d", got)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunFailed {
		t.Fatalf("run is not failed: %+v", runs)
	}
	if reported, err := f.db.GraphFailureReported(t.Context(), runs[0].ID, turn.PlanFailureNode); err != nil || reported {
		t.Fatalf("failure reported without delivery: %v %v", reported, err)
	}

	f.forge.onPostComment = nil
	// The retained comment's bytes are what a lost-reply recovery redelivers: make
	// the pending delivery visible on the forge as the bot's own comment.
	f.restorePlanFailureDelivery(runs[0])
	if !settleUntilHandled(f, planFailureOwnerComment, 8) {
		t.Fatalf("owner comment never handled after the failure comment was delivered: %d logs=%s", f.watermark(), f.logs)
	}
	if reported, err := f.db.GraphFailureReported(t.Context(), runs[0].ID, turn.PlanFailureNode); err != nil || !reported {
		t.Fatalf("failure comment not reported after recovery: %v %v", reported, err)
	}
	// The comment reached the issue once, as the restored delivery; recovery
	// never posted a second copy through the API.
	if got := len(failureCommentsOnForge(f)); got != 0 {
		t.Fatalf("recovery reposted a delivery that had reached the issue: %d API posts", got)
	}
	f.forge.mu.Lock()
	onIssue := 0
	for _, c := range f.forge.state(testOwnerName, testRepoName).comments {
		if strings.Contains(c.Body, "Plan could not finish this step") {
			onIssue++
		}
	}
	f.forge.mu.Unlock()
	if onIssue != 1 {
		t.Fatalf("expected exactly one failure comment on the issue, got %d", onIssue)
	}
}

// restorePlanFailureDelivery models a failure comment that reached the issue
// although its reply was lost: the retained delivery's exact bytes appear as the
// bot's comment, which is the evidence the reporter accepts.
func (f *fixture) restorePlanFailureDelivery(run store.Run) {
	f.t.Helper()
	attempts, err := f.db.ListGraphNodeAttemptsByRun(f.t.Context(), run.ID)
	if err != nil || len(attempts) == 0 {
		f.t.Fatalf("no attempts for the run: %v", err)
	}
	comments, err := f.db.ListGraphComments(f.t.Context(), attempts[0].InstanceID, "", 10)
	if err != nil || len(comments) != 1 {
		f.t.Fatalf("retained failure comment: %+v %v", comments, err)
	}
	delivery, err := f.db.GetReportDelivery(f.t.Context(), comments[0].Key)
	if err != nil {
		f.t.Fatal(err)
	}
	f.forge.addCommentAt(testIssue, 900000+run.TriggerCommentID, bot(), delivery.Body, f.clock.Now())
}

// The daemon's own guard: a failed graph Plan run that comes back with no error
// and no published failure comment must not spend the owner's comment. Only once
// the run has its failure comment is the comment handled.
func TestPlanFailureDaemon_FailedRunWithoutFailureCommentIsNeverHandled(t *testing.T) {
	p := &planFailureTurn{outcome: harness.OutcomeFailed, cause: "the provider refused the request", silent: true}
	f := planFailureFixture(t, p)
	for range 4 {
		f.clock.advance(time.Minute)
		f.settle()
		if got := f.watermark(); got >= planFailureOwnerComment {
			t.Fatalf("owner comment handled for a failed run with no failure comment: watermark=%d logs=%s", got, f.logs)
		}
	}
	if p.turnCalls() < 2 {
		t.Fatalf("the unhandled comment was not retried: %d turns", p.turnCalls())
	}
	if !strings.Contains(f.logs.String(), "no published failure comment") {
		t.Fatalf("the daemon did not say why the comment stays unhandled: %s", f.logs)
	}
	runs := f.runs()
	if len(runs) != 1 {
		t.Fatalf("retries allocated extra runs: %+v", runs)
	}
	// The failure comment is published (by the run's replay), and only then is the
	// owner's comment handled.
	p.mu.Lock()
	p.silent = false
	p.mu.Unlock()
	if !settleUntilHandled(f, planFailureOwnerComment, 6) {
		t.Fatalf("owner comment not handled once the failure comment was published: %d logs=%s", f.watermark(), f.logs)
	}
	if got := len(failureCommentsOnForge(f)); got != 1 {
		t.Fatalf("expected one failure comment, got %d", got)
	}
}

// A story child issue's owner reply reaches the story's own Plan task. Its own
// watermark advances only with the failure comment: a failed run whose failure
// comment is not published leaves the reply pending, and the retry that has
// published it is what handles it.
func TestPlanFailureDaemon_ChildReplyIsHandledOnlyWithTheFailureComment(t *testing.T) {
	for _, tt := range []struct {
		name   string
		silent bool
	}{
		{"failure comment published by the failing run", false},
		{"failed run without a failure comment", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &planFailureTurn{outcome: harness.OutcomeFailed, cause: "the provider refused the request", silent: tt.silent}
			f := newFixture(t)
			p.f = f
			f.turns.fn = p.execute
			f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
			f.plantCursor()
			f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent Plan", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
			f.settle()
			session := f.sessionOf(testIssue)
			if _, err := f.db.AdmitPlanWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			seedPlanChild(t, f, session)
			f.forge.addComment(200, 60, owner(), "/approve-plan story story candidate")

			childCursor := func() int64 { return storyWatermark(f, 200) }
			handledOnly := func() {
				t.Helper()
				if childCursor() >= 60 && len(failureCommentsOnForge(f)) == 0 {
					t.Fatalf("child reply handled with no failure comment published: logs=%s", f.logs)
				}
			}
			for range 4 {
				f.clock.advance(time.Minute)
				f.settle()
				handledOnly()
				if tt.silent && childCursor() >= 60 {
					t.Fatalf("child reply handled although the failed run has no failure comment: logs=%s", f.logs)
				}
			}
			if tt.silent {
				if p.turnCalls() < 2 {
					t.Fatalf("pending child reply not retried: %d turns", p.turnCalls())
				}
				p.mu.Lock()
				p.silent = false
				p.mu.Unlock()
				for range 6 {
					f.clock.advance(time.Minute)
					f.settle()
					handledOnly()
				}
			}
			if childCursor() < 60 {
				t.Fatalf("child reply never handled: cursor=%d logs=%s", childCursor(), f.logs)
			}
			bodies := failureCommentsOnForge(f)
			if len(bodies) != 1 {
				t.Fatalf("expected one failure comment, got %d", len(bodies))
			}
			requireFailureText(t, bodies[0], "the provider refused the request")
			if f.watermark() != 0 {
				t.Fatalf("child reply advanced the parent watermark: %d", f.watermark())
			}
			calls := p.turnCalls()
			f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
			f.clock.advance(time.Minute)
			f.settle()
			if p.turnCalls() != calls {
				t.Fatalf("handled child reply was dispatched again after restart: %d -> %d", calls, p.turnCalls())
			}
		})
	}
}

func seedPlanChild(t *testing.T, f *fixture, session store.Session) {
	t.Helper()
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
	binding.IssueID, binding.IssueNumber = 2000, 200
	binding.Revision = turn.PlanIssueRevision(title, body)
	binding.Candidate, binding.DraftJSON = "candidate", string(raw)
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
	// The story issue is a task of its own, met before the owner comments on it,
	// and the real turn admits its graph route on its first run.
	story, err := f.db.GetOrCreateSession(t.Context(), f.repo, 200)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.BootstrapWatermark(t.Context(), story.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdmitPlanWorkflowPath(t.Context(), story.ID, "graph"); err != nil {
		t.Fatal(err)
	}
}

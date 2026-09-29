package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// bugExhaustionReplyEnv is one real, store-backed exhaustion question: a real
// session, bug lifecycle and graph instance driven through one real analyst
// dispatch, then an exhaustion recorded and its question posted and confirmed
// through the exact production calls publishExhaustionQuestion itself uses
// (RecordBugExhaustion, BugRepairHistory, BugParentPolicy.PublishEvent,
// RecordBugLifecycleDisposition) rather than by driving three full repair
// attempts end to end through the graph. continueOwner/continueExhausted are
// then exercised against this real state, never a fabricated struct literal.
type bugExhaustionReplyEnv struct {
	f          *fixture
	forge      *fakeForge
	runtime    *BugRuntime
	req        Request
	life       store.BugLifecycle
	n          int
	questionID int64
}

func newBugExhaustionReplyEnv(t *testing.T) bugExhaustionReplyEnv {
	t.Helper()
	var session int64
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		raw, _ := json.Marshal(map[string]any{"version": 1, "response": "blocked", "result": map[string]any{"outcome": "blocked", "reason": "Controlled fixture lacks reproduction evidence", "comment": "Investigation retained; no repair is authorized.", "payload": map[string]any{"bug": map[string]any{"response": "blocked", "invocation": fmt.Sprintf("analysis:%d:%d", session, req.Graph.AttemptID), "base_revision": 0, "evidence": []string{"controlled owner report"}}}}})
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(raw), FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	session = f.session.ID
	f.opts.Tiers.ModelByTier[config.Kind("claude")][config.Tier("flagship")] = "fixture-model"
	req.Workflow = agent.WorkflowBug
	req.Mode = ""
	req.Command = ParseCommand("Investigate this bug")
	req.Trigger.Body = "Investigate this bug"
	req.ToolProfile = "bug-analyst"
	if _, e := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session, Version: 1, Family: "standalone", Workflow: "bug"}); e != nil {
		t.Fatal(e)
	}
	runtime, e := NewBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(_ agent.Kind, role string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read", "Glob", "Grep"}, AllowedTools: []string{"Read", "Glob", "Grep"}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	f.opts.Bug = runtime
	if _, e = Execute(t.Context(), f.opts, req); e != nil {
		t.Fatal(e)
	}
	life, e := f.db.GetBugLifecycle(t.Context(), session)
	if e != nil {
		t.Fatal(e)
	}
	instance, e := f.db.GetGraphInstance(t.Context(), life.ParentInstance)
	if e != nil {
		t.Fatal(e)
	}
	// ClaimBugContinuation (which DecideBugExhaustion's grant path uses) only
	// admits a "budget-grant" continuation once the parent graph instance is no
	// longer running -- real exhaustion only ever reaches the owner after the
	// attempt node's own outcome already carried the parent to a terminal
	// status via report -> stopped. Force that same terminal transition here
	// with one real (start, complete) attempt pair on the instance's actual
	// current node, exactly the shape store-level exhaustion fixtures use,
	// instead of running three full fixer/test cycles end to end.
	forced, e := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, StartedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	// RetainBugLifecycleReport (used by PublishEvent for a "question" report)
	// requires its source attempt to have actually succeeded, matching the real
	// attempt node reaching its "exhausted" outcome before report runs.
	if e = f.db.CompleteGraphNodeAttempt(t.Context(), forced.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "exhausted", FinishedAt: time.Now(), InstanceStatus: store.GraphSucceeded}); e != nil {
		t.Fatal(e)
	}
	attemptID := forced.ID

	n, e := f.db.RecordBugExhaustion(t.Context(), session, attemptID)
	if e != nil {
		t.Fatal(e)
	}
	history, e := f.db.BugRepairHistory(t.Context(), session)
	if e != nil {
		t.Fatal(e)
	}
	event := fmt.Sprintf("exhaustion:%d", n)
	reporter := BugParentPolicy{Reporter: f.opts.ResearchReporter}
	if e = reporter.PublishEvent(t.Context(), session, attemptID, event, "question", bugExhaustionQuestionComment(n, history)); e != nil {
		t.Fatal(e)
	}
	if e = f.db.RecordBugLifecycleDisposition(t.Context(), session, life.ParentInstance, "report", "exhausted", event); e != nil {
		t.Fatal(e)
	}
	life, e = f.db.GetBugLifecycle(t.Context(), session)
	if e != nil {
		t.Fatal(e)
	}
	if life.Disposition != "exhausted" || life.Reference != event {
		t.Fatalf("exhaustion seed failed: %+v", life)
	}
	questionID, e := f.db.BugExhaustionQuestionComment(t.Context(), session, n)
	if e != nil {
		t.Fatal(e)
	}
	if questionID <= 0 {
		t.Fatal("exhaustion question was not confirmed with a real posted comment id")
	}
	return bugExhaustionReplyEnv{f: f, forge: forge, runtime: runtime, req: req, life: life, n: n, questionID: questionID}
}

// TestBugRuntimeContinueExhausted_QuestionPending is item 1: a real exhaustion
// reaches disposition "exhausted" with its question recorded and confirmed
// against an actual posted comment.
func TestBugRuntimeContinueExhausted_QuestionPending(t *testing.T) {
	t.Parallel()
	env := newBugExhaustionReplyEnv(t)
	if env.life.Disposition != "exhausted" {
		t.Fatalf("disposition = %q, want exhausted", env.life.Disposition)
	}
	if env.life.Reference != fmt.Sprintf("exhaustion:%d", env.n) {
		t.Fatalf("reference = %q, want exhaustion:%d", env.life.Reference, env.n)
	}
	found := false
	for _, p := range env.forge.posts {
		if strings.Contains(p.body, "repair budget is spent") {
			found = true
		}
	}
	if !found {
		t.Fatal("the exhaustion question was never actually posted")
	}
}

// TestBugRuntimeContinueExhausted_GrantMatchingN is item 2: a grant reply for
// the current exhaustion number, posted after the question, claims a fresh
// continuation parked on the attempt node and clears the exhausted disposition.
func TestBugRuntimeContinueExhausted_GrantMatchingN(t *testing.T) {
	t.Parallel()
	env := newBugExhaustionReplyEnv(t)
	req := env.req
	req.Trigger = Trigger{CommentID: env.questionID + 1, AuthorID: 42, Body: fmt.Sprintf("/grant-bug-attempt %d focus on the retry path", env.n)}

	next, err := env.runtime.continueOwner(t.Context(), req, env.life)
	if err != nil {
		t.Fatalf("grant refused: %v", err)
	}
	if next.CurrentNode != "attempt" || next.Status != store.GraphRunning {
		t.Fatalf("grant successor = %+v, want a fresh running instance parked on attempt", next)
	}
	life, err := env.f.db.GetBugLifecycle(t.Context(), env.f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if life.Disposition != "running" || life.ParentInstance != next.ID {
		t.Fatalf("lifecycle did not resume onto the granted successor: %+v (want parent %d)", life, next.ID)
	}
}

// TestBugRuntimeContinueExhausted_StopMatchingN is item 3.
//
// continueExhausted's stop branch records the "owner-stopped" decision (via
// DecideBugExhaustion), resolves the real source attempt that raised the
// exhaustion question (BugExhaustionSourceAttempt) and then posts its
// acknowledgement against that real attempt id, matching every other
// PublishEvent call site in this package.
//
// Net effect: the decision commits and the lifecycle moves to "owner-stopped"
// (so the intended terminal outcome and "no branch/PR action taken" both
// hold), continueOwner returns no error, the parent graph instance is left
// untouched, and the "Stopped." acknowledgement comment is actually posted.
func TestBugRuntimeContinueExhausted_StopMatchingN(t *testing.T) {
	t.Parallel()
	env := newBugExhaustionReplyEnv(t)
	before, err := env.f.db.GetGraphInstance(t.Context(), env.life.ParentInstance)
	if err != nil {
		t.Fatal(err)
	}
	req := env.req
	req.Trigger = Trigger{CommentID: env.questionID + 1, AuthorID: 42, Body: fmt.Sprintf("/stop-bug %d", env.n)}

	_, err = env.runtime.continueOwner(t.Context(), req, env.life)
	if err != nil {
		t.Fatalf("stop refused: %v", err)
	}
	life, err := env.f.db.GetBugLifecycle(t.Context(), env.f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if life.Disposition != "owner-stopped" {
		t.Fatalf("disposition = %q, want owner-stopped", life.Disposition)
	}
	after, err := env.f.db.GetGraphInstance(t.Context(), env.life.ParentInstance)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID || after.CurrentNode != before.CurrentNode || after.Status != before.Status {
		t.Fatalf("stop touched the parent graph instance: got %+v, want unchanged %+v", after, before)
	}
	found := false
	for _, p := range env.forge.posts {
		if strings.Contains(p.body, "Stopped.") {
			found = true
		}
	}
	if !found {
		t.Fatal("the stop acknowledgement was never actually posted")
	}
}

// TestBugRuntimeContinueExhausted_NonOwnerRefused is item 4: neither command
// from a non-owner author is accepted; the disposition stays exhausted and the
// refusal names both commands.
func TestBugRuntimeContinueExhausted_NonOwnerRefused(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"/grant-bug-attempt 1 please retry",
		"/stop-bug 1",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			env := newBugExhaustionReplyEnv(t)
			req := env.req
			req.Trigger = Trigger{CommentID: env.questionID + 1, AuthorID: 999, Body: body}

			_, err := env.runtime.continueOwner(t.Context(), req, env.life)
			if err == nil || err.Error() != bugExhaustionNotWaitingText {
				t.Fatalf("non-owner reply err = %v, want the not-waiting refusal naming both commands", err)
			}
			life, e := env.f.db.GetBugLifecycle(t.Context(), env.f.session.ID)
			if e != nil {
				t.Fatal(e)
			}
			if life.Disposition != "exhausted" {
				t.Fatalf("disposition moved on a refused non-owner reply: %q", life.Disposition)
			}
		})
	}
}

// TestBugRuntimeContinueExhausted_CommentPredatesQuestionRefused is item 5: an
// owner comment at or before the question's own comment id can never answer it.
func TestBugRuntimeContinueExhausted_CommentPredatesQuestionRefused(t *testing.T) {
	t.Parallel()
	for _, delta := range []int64{0, -1} {
		t.Run(fmt.Sprintf("delta_%d", delta), func(t *testing.T) {
			t.Parallel()
			env := newBugExhaustionReplyEnv(t)
			req := env.req
			req.Trigger = Trigger{CommentID: env.questionID + delta, AuthorID: 42, Body: fmt.Sprintf("/grant-bug-attempt %d retry", env.n)}

			_, err := env.runtime.continueOwner(t.Context(), req, env.life)
			if err == nil || err.Error() != bugExhaustionNotWaitingText {
				t.Fatalf("stale-comment reply err = %v, want the not-waiting refusal", err)
			}
		})
	}
}

// TestBugRuntimeContinueExhausted_UnrecognizedOrWrongNRefused is item 6: an
// unrecognized command, or either command naming a stale/future exhaustion
// number, is refused with the same message.
func TestBugRuntimeContinueExhausted_UnrecognizedOrWrongNRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"unrecognized command", "/approve-bug analysis 1"},
		{"grant wrong n", "/grant-bug-attempt 2 retry"},
		{"stop wrong n", "/stop-bug 2"},
		{"grant missing n", "/grant-bug-attempt"},
		{"grant non-numeric n", "/grant-bug-attempt abc retry"},
		{"stop missing n", "/stop-bug"},
		{"stop non-numeric n", "/stop-bug abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := newBugExhaustionReplyEnv(t)
			req := env.req
			req.Trigger = Trigger{CommentID: env.questionID + 1, AuthorID: 42, Body: tc.body}

			_, err := env.runtime.continueOwner(t.Context(), req, env.life)
			if err == nil || err.Error() != bugExhaustionNotWaitingText {
				t.Fatalf("body %q err = %v, want the not-waiting refusal", tc.body, err)
			}
			life, e := env.f.db.GetBugLifecycle(t.Context(), env.f.session.ID)
			if e != nil {
				t.Fatal(e)
			}
			if life.Disposition != "exhausted" {
				t.Fatalf("disposition moved on a refused reply: %q", life.Disposition)
			}
		})
	}
}

// TestBugRuntimeContinueExhausted_ReplaySameCommentIdempotent is item 7: the
// exact same owner decision comment processed twice through the wiring layer
// produces exactly one recorded decision and one continuation, matching
// DecideBugExhaustion's own idempotency at the store layer.
func TestBugRuntimeContinueExhausted_ReplaySameCommentIdempotent(t *testing.T) {
	t.Parallel()
	env := newBugExhaustionReplyEnv(t)
	req := env.req
	req.Trigger = Trigger{CommentID: env.questionID + 1, AuthorID: 42, Body: fmt.Sprintf("/grant-bug-attempt %d retry once more", env.n)}

	first, err := env.runtime.continueOwner(t.Context(), req, env.life)
	if err != nil {
		t.Fatalf("first grant: %v", err)
	}
	// The same stale "exhausted" life snapshot is reused deliberately: it is
	// exactly what a crash-repeated request replays with, and continueExhausted
	// re-derives idempotency from the store (DecideBugExhaustion), not from life.
	second, err := env.runtime.continueOwner(t.Context(), req, env.life)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay produced a distinct successor: first=%d second=%d", first.ID, second.ID)
	}
	history, err := env.f.db.BugRepairHistory(t.Context(), env.f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Exhaustions) != 1 || history.Exhaustions[0].Decision != "grant" {
		t.Fatalf("replay recorded a second decision: %+v", history.Exhaustions)
	}
}

// TestBugRuntimeContinueExhausted_CancelBeforeGrantRefused is item 8. It pins
// the actual, current behavior: CancelBugLifecycle unconditionally overwrites
// bug_tasks.disposition to "cancelled" regardless of what it held before, and
// BugRuntime's own admission path (bug_runtime.go) always re-fetches a fresh
// BugLifecycle immediately before calling continueOwner. So a cancel that lands
// before the owner's grant reply arrives means continueOwner never even enters
// continueExhausted for that reply -- life.Disposition is "cancelled", not
// "exhausted", by the time it is read.
//
// This is a real, working protection end to end, but note what it is *not*:
// continueExhausted itself contains no direct cancellation check (it never
// reads life.Cancelled). The refusal here is entirely a side effect of the
// disposition transition plus the single call site's fresh-fetch discipline.
// A hypothetical caller that retained and replayed an older "exhausted"
// snapshot captured before the cancel would not be stopped by
// continueExhausted itself.
func TestBugRuntimeContinueExhausted_CancelBeforeGrantRefused(t *testing.T) {
	t.Parallel()
	env := newBugExhaustionReplyEnv(t)
	if err := env.f.db.CancelBugLifecycle(t.Context(), env.f.session.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := env.f.db.GetBugLifecycle(t.Context(), env.f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Disposition != "cancelled" || !fresh.Cancelled {
		t.Fatalf("cancellation did not flip the lifecycle: %+v", fresh)
	}
	req := env.req
	req.Trigger = Trigger{CommentID: env.questionID + 1, AuthorID: 42, Body: fmt.Sprintf("/grant-bug-attempt %d retry", env.n)}

	if _, err = env.runtime.continueOwner(t.Context(), req, fresh); err == nil {
		t.Fatal("a grant was accepted for a cancelled Bug task")
	}
	after, err := env.f.db.GetBugLifecycle(t.Context(), env.f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Disposition != "cancelled" {
		t.Fatalf("grant partially applied despite refusal: %+v", after)
	}
}

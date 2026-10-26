package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestFirstSightBootstrapConsumesNothing is AC-2.10. A task litefactory has
// never seen has its watermark planted at the highest comment that already
// exists, and none of them is processed — otherwise registering a repository
// with a long-running issue replays every `/spike` anyone ever wrote on it.
func TestFirstSightBootstrapConsumesNothing(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addComment(testIssue, 10, owner(), "/spike do the first thing")
	f.forge.addComment(testIssue, 20, stranger(), "any progress?")
	f.forge.addComment(testIssue, 30, owner(), "/spike do the second thing")

	result := f.settle()

	if result.Dispatched != 1 {
		t.Errorf("dispatched %d tasks, want 1 — a first sight is work like any other", result.Dispatched)
	}
	if got := f.watermark(); got != 30 {
		t.Errorf("watermark is %d, want 30 — the highest comment that already existed", got)
	}
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns from a task's history, want none", len(calls))
	}
	if _, _, posts, reactions := f.forge.counts(); posts != 0 || reactions != 0 {
		t.Errorf("posted %d comments and %d reactions on first sight, want none", posts, reactions)
	}
}

// TestInstructionRunsAndIsReported walks the whole ordinary path: a new comment
// from the owner is acknowledged, run and reported, and the watermark moves
// past it in the same transaction that recorded the report.
func TestInstructionRunsAndIsReported(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight

	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")
	result := f.settle()

	if result.Dispatched != 1 {
		t.Fatalf("dispatched %d turns, want 1\nlogs:\n%s", result.Dispatched, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{40}) {
		t.Fatalf("turn triggers %v, want [40]\nlogs:\n%s", got, f.logs)
	}
	if got := f.turns.calls()[0].Command.Text; got != "add a health endpoint" {
		t.Errorf("instruction text is %q", got)
	}

	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one report", len(posted))
	}
	if !strings.HasPrefix(posted[0], turn.MarkerPrefix) {
		t.Errorf("report does not begin with the litefactory marker:\n%s", posted[0])
	}

	// AC-2.17: the acknowledgement is on the triggering comment, and it happens
	// before the run rather than alongside the report.
	if !slices.Equal(reacted, []int64{40}) {
		t.Errorf("reacted to %v, want [40]", reacted)
	}
	order := f.forge.orderFor(testIssue)
	reactAt := slices.Index(order, "react:40")
	turnAt := slices.Index(order, "turn:40")
	if reactAt < 0 || turnAt < 0 || reactAt > turnAt {
		t.Errorf("the reaction must come before the run; order was %v", order)
	}

	if got := f.watermark(); got != 40 {
		t.Errorf("watermark is %d, want 40", got)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunReported {
		t.Fatalf("runs after the tick: %+v", runs)
	}
	if runs[0].ResultCommentID == 0 {
		t.Error("the run has no result comment id")
	}
}

// TestFreeTextRunsWithTheModeLeftToTheAgent is the whole of the comment-intent
// change seen from the outside: an owner comment that names no command starts a
// run just as /spike does, carries the entire body as its instruction, and
// leaves the question of what kind of work this is to the run itself.
func TestFreeTextRunsWithTheModeLeftToTheAgent(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight

	const body = "draft some mockups for the settings screen\n\n- one per state"
	f.forge.addComment(testIssue, 41, owner(), body)
	result := f.settle()

	if result.Dispatched != 1 {
		t.Fatalf("dispatched %d turns, want 1\nlogs:\n%s", result.Dispatched, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{41}) {
		t.Fatalf("turn triggers %v, want [41]\nlogs:\n%s", got, f.logs)
	}

	call := f.turns.calls()[0]
	if call.Command.Kind != turn.KindFreeText {
		t.Errorf("comment classified as %v, want %v", call.Command.Kind, turn.KindFreeText)
	}
	// The whole body, not a remainder: there was no command word to strip.
	if call.Command.Text != body {
		t.Errorf("instruction text is %q, want the whole body %q", call.Command.Text, body)
	}
	if call.Command.Name != "" {
		t.Errorf("free text named a command: %q", call.Command.Name)
	}

	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 1 || !strings.HasPrefix(posted[0], turn.MarkerPrefix) {
		t.Fatalf("posted %d comments, want exactly one report", len(posted))
	}
	if !slices.Equal(reacted, []int64{41}) {
		t.Errorf("reacted to %v, want [41]", reacted)
	}
	if got := f.watermark(); got != 41 {
		t.Errorf("watermark is %d, want 41", got)
	}
}

// TestDaemonDoesNotReactToItsOwnComment is AC-2.14, asserted the way the
// criterion asks for it: the daemon is fed a comment it posted itself.
func TestDaemonDoesNotReactToItsOwnComment(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("expected one report to feed back, got %d", len(posted))
	}
	// The daemon's own report, arriving as an inbound comment. Its author is
	// the bot's numeric id, and its body carries the marker: either alone is
	// enough for the filter.
	f.forge.addComment(testIssue, 50, bot(), posted[0])

	before := len(f.turns.calls())
	f.settle()

	if got := len(f.turns.calls()); got != before {
		t.Errorf("ran %d turns from litefactory's own comment, want none", got-before)
	}
	if got := f.watermark(); got != 50 {
		t.Errorf("watermark is %d, want 50 — its own comment is consumed, not left to be re-seen", got)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Errorf("posted %d comments; the daemon answered itself", len(posted))
	}

	var self store.Comment
	for _, c := range f.comments() {
		if c.ForgeCommentID == 50 {
			self = c
		}
	}
	if self.Classification != store.ClassificationSelf {
		t.Errorf("own comment classified %q, want %q", self.Classification, store.ClassificationSelf)
	}
}

// TestOwnerQuoteReplyStartsARun is AC-S0.1 end to end, against the shape that
// produced it: litefactory posts a report, the owner presses GitHub's "quote
// reply" on it and answers underneath, and the answer has to start a run.
//
// It used to start nothing. The quoted marker matched the self-filter's
// substring search, the owner's comment was classified as litefactory's own
// output, and it was dropped without a run, a reply or even a reaction — so
// answering a litefactory question worked or did not depending on which button
// was pressed.
func TestOwnerQuoteReplyStartsARun(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight

	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("expected one report to quote, got %d", len(posted))
	}

	// Every line of litefactory's report prefixed with "> ", then the owner's
	// own instruction underneath: what the quote-reply button writes.
	quoted := "> " + strings.ReplaceAll(posted[0], "\n", "\n> ") +
		"\n\nnow do the same for the readiness endpoint"
	f.forge.addComment(testIssue, 41, owner(), quoted)

	f.settle()
	if got := f.turns.triggers(); !slices.Contains(got, int64(41)) {
		t.Errorf("turn triggers %v, want the quote-reply (41) among them", got)
	}

	// The body reaches the run whole, quote included. Stripping the quote is not
	// litefactory's job: the run reads the comment the owner wrote.
	call := f.turns.calls()[len(f.turns.calls())-1]
	if !strings.Contains(call.Command.Text, "readiness endpoint") {
		t.Errorf("the run did not receive the owner's instruction; got %q", call.Command.Text)
	}

	var recorded store.Comment
	for _, c := range f.comments() {
		if c.ForgeCommentID == 41 {
			recorded = c
		}
	}
	if recorded.Classification != store.ClassificationInstruction {
		t.Errorf("the quote-reply was classified %q, want %q",
			recorded.Classification, store.ClassificationInstruction)
	}
}

// TestOwnCommentUnderAHumanAccountIsStillDropped covers the belt-and-braces
// half of the self-filter: the marker catches a report even when the account it
// was posted from is not the one the identities table names.
func TestOwnCommentUnderAHumanAccountIsStillDropped(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 41, owner(),
		turn.MarkerPrefix+"status task=github:acme/acme-api#123 run=1 -->\n\nRun 1 succeeded.")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns from a marked comment, want none", len(calls))
	}
	if got := f.watermark(); got != 41 {
		t.Errorf("watermark is %d, want 41", got)
	}
}

// TestCommentsAreProcessedInAscendingIDOrder is AC-2.11. Processing a `/spike`
// before the comment that supersedes it would be a silent, expensive bug, so
// the order is imposed rather than assumed.
func TestCommentsAreProcessedInAscendingIDOrder(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	// Added out of order, as an eventually consistent API might return them.
	f.forge.addComment(testIssue, 70, owner(), "/spike third")
	f.forge.addComment(testIssue, 50, owner(), "/spike first")
	f.forge.addComment(testIssue, 60, owner(), "/spike second")

	f.settle()
	f.settle()
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{50, 60, 70}) {
		t.Errorf("turns ran for %v, want [50 60 70]", got)
	}
	if got := f.watermark(); got != 70 {
		t.Errorf("watermark is %d, want 70", got)
	}
}

// TestQueueStopsAtTheFirstCommentWhoseReportFailed is the constraint this whole
// package is arranged around.
//
// store.AdvanceWatermark takes a MAX and never rewinds. If the loop kept
// processing a task's later comments after one of them failed to report, the
// next successful consume would put the watermark past the unreported comment
// for ever: the report would never be retried, and nothing would say so — the
// watermark would look healthy and the missing comment would simply never be
// noticed.
func TestQueueStopsAtTheFirstCommentWhoseReportFailed(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 11, owner(), "/spike first")
	f.forge.addComment(testIssue, 12, owner(), "/spike second")
	f.forge.addComment(testIssue, 13, owner(), "/spike third")

	// The first post — the report for comment 11 — is refused.
	f.forge.onPostComment = func(w http.ResponseWriter, call int) bool {
		if call != 1 {
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}

	bootstrapped := f.watermark()
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{11}) {
		t.Fatalf("turns ran for %v; the queue must stop at comment 11, not go on to 12 and 13", got)
	}
	if got := f.watermark(); got != bootstrapped {
		t.Errorf("watermark moved to %d after a failed report; it must stay at %d", got, bootstrapped)
	}
	if session := f.session(); !session.NeedsPoll {
		t.Errorf("needs_poll is clear after a queue that stopped early; the flag is what a "+
			"monotonic cursor leaves behind to come back for\nlogs:\n%s", f.logs)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].Status == store.RunReported {
		t.Errorf("runs after the failed report: %+v; the run must stay unreported", runs)
	}

	// The poll cursor has moved past this issue's update time, so the listings
	// cannot bring it back. The backlog flag is what does, and it is the whole
	// point of AC-PAR-5.2: no change to UpdatedAt, and the comment is still
	// found again.
	f.forge.onPostComment = nil
	f.restorePendingReport(11)
	f.settle()
	f.settle()
	f.settle()
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{11, 12, 13}) {
		t.Errorf("turns ran for %v, want [11 12 13]: delivery 11 recovered without rerun, then 12 and 13", got)
	}
	if got := f.watermark(); got != 900011 {
		t.Errorf("watermark is %d after the retry, want 900011 (the recovered receipt after all owner commands)", got)
	}
	if runs := f.runs(); len(runs) != 3 {
		t.Errorf("allocated %d runs, want 3 — comment 11 must not run twice", len(runs))
	}
	for _, run := range f.runs() {
		if run.Status != store.RunReported {
			t.Errorf("run %d is %q, want %q", run.Ordinal, run.Status, store.RunReported)
		}
	}
}

// scriptedReporter wraps the real reporter and can fail one report the way the
// expensive case fails: the comment reached the forge, and the transaction that
// records it did not commit.
type scriptedReporter struct {
	inner  daemon.Reporter
	failOn int64
	failed bool
}

func (r *scriptedReporter) ReportTurn(ctx context.Context, result turn.Result) (turn.Posted, error) {
	if result.TriggerCommentID == r.failOn && !r.failed {
		r.failed = true
		// Posted.CommentID is set as soon as the post returns, so a non-zero id
		// alongside an error means the comment is on the issue and litefactory
		// does not know it.
		return turn.Posted{CommentID: 987_654}, errors.New("record the report: database is locked")
	}
	return r.inner.ReportTurn(ctx, result)
}

func (r *scriptedReporter) ReportRun(ctx context.Context, rep turn.RunReport) (turn.Posted, error) {
	return r.inner.ReportRun(ctx, rep)
}

func (r *scriptedReporter) ReportReply(ctx context.Context, req turn.ReplyRequest) (turn.Posted, error) {
	return r.inner.ReportReply(ctx, req)
}

func (r *scriptedReporter) Consume(ctx context.Context, sessionID, commentID int64) (store.Watermark, error) {
	return r.inner.Consume(ctx, sessionID, commentID)
}

// TestQueueStopsWhenAPostedReportWasNotRecorded is the same constraint through
// its nastiest door: the post succeeded, the recording did not.
//
// A caller that read "there is a comment id, so it was posted" would carry on
// to the next comment and lose the first for ever. Only the watermark knows,
// and the watermark is what the loop asks.
func TestQueueStopsWhenAPostedReportWasNotRecorded(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 21, owner(), "/spike first")
	f.forge.addComment(testIssue, 22, owner(), "/spike second")

	bootstrapped := f.watermark()
	reporter := &scriptedReporter{inner: f.reporter, failOn: 21}
	f.daemon = f.newDaemon(daemon.Options{Reporter: reporter})

	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{21}) {
		t.Fatalf("turns ran for %v; comment 22 must not be touched after 21 was not consumed", got)
	}
	if got := f.watermark(); got != bootstrapped {
		t.Errorf("watermark is %d, want %d: nothing may be consumed past an unrecorded report",
			got, bootstrapped)
	}
	if !strings.Contains(f.logs.String(), "report recovery pending") {
		t.Error("the failed bookkeeping was not surfaced as pending recovery")
	}
}

// TestLockedTaskLeavesTheCommentUnconsumed is AC-3.1: no queue, no "busy"
// reply, and the comment comes back on a later tick.
func TestLockedTaskLeavesTheCommentUnconsumed(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 31, owner(), "/spike while the task is busy")
	f.forge.addComment(testIssue, 32, owner(), "/spike and again")

	bootstrapped := f.watermark()
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		return f.turnResult(ctx, req, turn.DispositionLocked, harness.OutcomeSucceeded)
	})

	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{31}) {
		t.Errorf("turns ran for %v, want [31]: a locked task stops its own queue", got)
	}
	if got := f.watermark(); got != bootstrapped {
		t.Errorf("watermark is %d, want %d: a contended lock consumes nothing", got, bootstrapped)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Errorf("posted %d comments for a locked task, want none — there is no busy reply", len(posted))
	}

	// A later tick retries, which is the whole point of not consuming it.
	f.turns.set(f.ranSuccessfully)
	f.settle()
	f.settle()
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{31, 31, 32}) {
		t.Errorf("turns ran for %v, want [31 31 32]", got)
	}
	if got := f.watermark(); got != 32 {
		t.Errorf("watermark is %d, want 32", got)
	}
}

// TestDeferredTaskDoesNotStallTheOthers checks that the constraint is per task
// and not per tick: one task holding a comment back must not stop another
// task's work in the same tick.
func TestDeferredTaskDoesNotStallTheOthers(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addIssue(fakeIssue{Number: 124, Title: "Another task", User: stranger()})
	f.settle()

	f.forge.addComment(testIssue, 61, owner(), "/spike on the locked task")
	f.forge.addComment(124, 62, owner(), "/spike on the free task")

	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == 61 {
			return f.turnResult(ctx, req, turn.DispositionLocked, harness.OutcomeSucceeded)
		}
		return f.ranSuccessfully(ctx, req)
	})

	f.settle()

	if session := f.sessionOf(testIssue); !session.NeedsPoll {
		t.Errorf("needs_poll is clear on the locked task; it consumed nothing and must be "+
			"reconsidered\nlogs:\n%s", f.logs)
	}
	if session := f.sessionOf(124); !session.NeedsPoll {
		t.Errorf("needs_poll is clear before the one-shot mode-label refresh\nlogs:\n%s", f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{61, 62}) {
		t.Errorf("turns ran for %v, want [61 62]: the second task is unaffected", got)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Errorf("posted %d comments, want 1 — the free task's report", len(posted))
	}
}

// An unassigned issue authored by the approver may bootstrap exactly once.
func TestAnUnassignedApproverIssueBodyRunsOnce(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.forge.addIssue(fakeIssue{Number: testIssue, Title: "Please do this", Body: "/spike add a health endpoint", User: owner()})
	f.settle()
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("approver-authored unassigned body triggers %v, want [0]", got)
	}
}

// Assignment cannot authorize a body authored by a third party.
func TestAnAssignedThirdPartyIssueBodyStartsNothing(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.assignedIssueBy(stranger(), "Third-party request", "/spike rewrite the authentication layer")
	f.settle()
	f.forge.addComment(testIssue, 141, stranger(), "bumping this")
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("third-party body ran %d turns", len(calls))
	}
	if posted, reacted, _ := f.forge.snapshot(); len(posted) != 0 || len(reacted) != 0 {
		t.Fatalf("third-party body produced comments=%d reactions=%d", len(posted), len(reacted))
	}
	if got := f.runs(); len(got) != 0 {
		t.Fatalf("third-party body recorded %d runs", len(got))
	}
}

// An approver-authored body remains the first instruction and runs only once.
func TestAnAssignedIssueBodyRunsOnceAsTheFirstInstruction(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	const body = "/spike add a /health endpoint that returns 200"
	f.assignedIssue("Add a /health endpoint", body)

	result := f.settle()

	if result.Dispatched != 1 {
		t.Errorf("dispatched %d tasks, want 1", result.Dispatched)
	}

	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("the executor was called %d times, want 1", len(calls))
	}
	req := calls[0]
	if req.Trigger.CommentID != 0 {
		t.Errorf("trigger comment id is %d, want 0: the body is not a comment, and any positive id "+
			"would advance the watermark past comments that have not been read", req.Trigger.CommentID)
	}
	if req.Trigger.Body != "" {
		t.Errorf("trigger body is %q, want empty: there is no comment, and the body reaches the "+
			"prompt as the instruction and as the issue note", req.Trigger.Body)
	}
	if req.Trigger.Author != ownerLogin {
		t.Errorf("trigger author is %q, want approver %q", req.Trigger.Author, ownerLogin)
	}
	if req.Command.Kind != turn.KindSpike {
		t.Errorf("the body parsed to %s, want %s", req.Command.Kind, turn.KindSpike)
	}
	if want := "add a /health endpoint that returns 200"; req.Command.Text != want {
		t.Errorf("instruction text is %q, want %q: the body itself is what the turn was asked to do",
			req.Command.Text, want)
	}
	if req.Issue.Body != body {
		t.Errorf("the request carried issue body %q, want %q", req.Issue.Body, body)
	}

	// AC-2.17 stays comment-only: there is no comment to react to, and an
	// issue-level reaction would be an eighth forge endpoint.
	if _, _, _, reactions := f.forge.counts(); reactions != 0 {
		t.Errorf("added %d reactions on the body path, want none — there is no comment to react to",
			reactions)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Fatalf("posted %d comments, want 1 — the run report", len(posted))
	}

	// And again. Nothing looks at the body a second time.
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Errorf("turns ran for %v, want [0]: the body fires exactly once", got)
	}
	if got := f.runs(); len(got) != 1 {
		t.Errorf("recorded %d runs, want 1", len(got))
	}
}

// TestFirstSightPlantsTheWatermarkBeforeTheBodyRuns is AC-2.10b.
//
// The ordering is the whole of "exactly once": the watermark exists before the
// turn starts, so a crash in the middle leaves a task that takes drain on the
// next tick and a `running` row that startup reconciliation reports as
// abandoned (AC-2.21). The alternative — run, then plant — would replay the
// body on every tick until it succeeded.
//
// It is asserted from inside the turn, because that is the only moment at which
// the question means anything.
func TestFirstSightPlantsTheWatermarkBeforeTheBodyRuns(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.assignedIssue("Add a /health endpoint", "/spike add a health endpoint")

	duringRun := int64(-2)
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		duringRun = f.watermark()
		return f.ranSuccessfully(ctx, req)
	})

	f.settle()

	if duringRun != 0 {
		t.Errorf("the watermark read %d while the body was running, want 0: it is planted first, "+
			"and at 0 on an issue that has no comments", duringRun)
	}
	if got := f.watermark(); got != 0 {
		t.Errorf("watermark is %d after the run, want 0", got)
	}
}

// TestABodyRunLeavesTheWatermarkWhereBootstrapPlantedIt is AC-2.23, and it is
// the criterion that catches the one bug this design exists to avoid.
//
// Reporting a run advances the watermark to the trigger id. Had the body been
// given the issue's own forge id — a plausible-looking six-digit number — that
// advance would have pushed the watermark past every comment the issue will
// ever receive, and the task would go permanently deaf the moment its body ran.
// With 0 the advance is MAX(0, 0) and changes nothing, so the comment that
// follows is read normally.
func TestABodyRunLeavesTheWatermarkWhereBootstrapPlantedIt(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.assignedIssue("Add a /health endpoint", "/spike add a health endpoint")

	f.settle()

	runs := f.runs()
	if len(runs) != 1 {
		t.Fatalf("recorded %d runs, want 1\nlogs:\n%s", len(runs), f.logs)
	}
	if runs[0].TriggerCommentID != 0 {
		t.Errorf("the runs row carries trigger_comment_id %d, want 0", runs[0].TriggerCommentID)
	}
	if runs[0].Status != store.RunReported {
		t.Errorf("the run is %s, want %s", runs[0].Status, store.RunReported)
	}
	if got := f.watermark(); got != 0 {
		t.Errorf("watermark is %d after reporting the body run, want 0: a body run must not "+
			"advance it past comments nobody has read", got)
	}

	// The part that bites: a comment written afterwards must still be seen.
	f.forge.addComment(testIssue, 617_004, owner(), "/spike and now the readiness probe")

	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{0, 617004}) {
		t.Errorf("turns ran for %v, want [0 617004]: the body run must not swallow later comments", got)
	}
	if got := f.watermark(); got != 617_004 {
		t.Errorf("watermark is %d, want 617004", got)
	}
}

// A Research-graph run started from a ticket body has trigger comment id 0 and
// nothing to consume. It must complete the body through bootstrap and leave
// the task's queue open for the next owner comment (issue #923, MAR-20).
func TestAResearchGraphBodyRunCompletesTheBodyAndKeepsTheQueueOpen(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "graph-body", false)
	w.plantCursor()
	w.assignedIssue("Graph body", "/spike add a health endpoint")
	w.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		result, err := w.ranSuccessfully(ctx, req)
		result.ResearchGraph = true
		return result, err
	})
	w.clock.advance(scheduleInterval)
	w.settle()

	if got := w.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("turns ran for %v, want [0]\nlogs:\n%s", got, w.fixture.logs)
	}
	if strings.Contains(w.fixture.logs.String(), "could not consume Research graph input") {
		t.Fatalf("the body graph run tried to consume a comment\nlogs:\n%s", w.fixture.logs)
	}
	id := w.session().ID
	if pending, err := w.db.TicketBodyPending(t.Context(), id); err != nil || pending {
		t.Fatalf("body still pending after the graph run: pending=%t, %v\nlogs:\n%s", pending, err, w.fixture.logs)
	}

	w.forge.addComment(testIssue, 923_001, owner(), "/spike and now the readiness probe")
	w.needsPoll(testIssue)
	w.clock.advance(testInterval)
	w.settle()

	if got := w.turns.triggers(); !slices.Equal(got, []int64{0, 923_001}) {
		t.Fatalf("turns ran for %v, want [0 923001]: the body run must not stop the queue\nlogs:\n%s", got, w.fixture.logs)
	}
	if got := w.watermark(); got != 923_001 {
		t.Fatalf("watermark is %d, want 923001", got)
	}
}

// TestABodyThatCommandsNothingStartsNothingAndPostsNothing is AC-2.16c.
//
// The silence is the point, and it is the one place the body path deliberately
// differs from the comment path. A `/status` in a comment draws a reply, because
// someone typed it at litefactory and deserves an answer. A `/plan` in an issue
// body may well be a heading in a template — replying to it would be
// litefactory answering a question nobody asked, on every issue anyone opens
// and assigns.
func TestABodyThatCommandsNothingStartsNothingAndPostsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "whitespace", body: "  \n\t "},
		{name: "a command from a later milestone", body: "/status"},
		{name: "a bare spike with no text", body: "/spike"},
		{name: "a command that is not in the vocabulary", body: "/frobnicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.assignedIssue("Add a /health endpoint", tc.body)

			result := f.settle()

			if result.Dispatched != 1 {
				t.Fatalf("dispatched %d tasks, want 1: the task is still created and watermarked",
					result.Dispatched)
			}
			if calls := f.turns.calls(); len(calls) != 0 {
				t.Errorf("the executor was called %d times, want none", len(calls))
			}
			posted, reacted, _ := f.forge.snapshot()
			if len(posted) != 0 {
				t.Errorf("posted %d comments in answer to an issue body, want none: %q", len(posted), posted)
			}
			if len(reacted) != 0 {
				t.Errorf("added %d reactions, want none", len(reacted))
			}
			if got := f.watermark(); got != 0 {
				t.Errorf("watermark is %d, want 0: the task is bootstrapped either way", got)
			}
		})
	}
}

// TestAnIssueWithCommentsDoesNotFireItsBody is AC-2.16d.
//
// A body is a *first* instruction. An issue that has already been talked on has
// a conversation to follow instead, and replaying its opening post underneath
// that conversation would be a second voice nobody asked for — quite possibly
// contradicting the comment that supersedes it. The comments drain exactly as
// they did before, which is to say the pre-existing ones do not run at all
// (AC-2.10) and the next one does.
func TestAnIssueWithCommentsDoesNotFireItsBody(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.assignedIssue("Add a /health endpoint", "/spike add a health endpoint")
	f.forge.addComment(testIssue, 220, stranger(), "is anyone looking at this?")

	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns on an issue that already has a comment, want none\nlogs:\n%s",
			len(calls), f.logs)
	}
	if got := f.watermark(); got != 220 {
		t.Errorf("watermark is %d, want 220 — the highest comment that already existed", got)
	}

	// And its comments go on working the way they always have.
	f.forge.addComment(testIssue, 221, owner(), "/spike do it now then")

	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{221}) {
		t.Errorf("turns ran for %v, want [221]: the comments drain as they do today", got)
	}
}

// Assigning a colleague cannot block a valid approver-authored body.
func TestAnApproverIssueBodyRunsWithAnotherAssignee(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.forge.addIssue(fakeIssue{Number: testIssue, Title: "Add a health endpoint", Body: "/spike add a health endpoint", User: owner(), Assignees: []fakeUser{stranger()}})
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("approver body with another assignee triggers %v, want [0]", got)
	}
}

// TestPullRequestsAreFilteredAndEachRepoCostsOneQuery is AC-2.8.
func TestPullRequestsAreFilteredAndEachRepoCostsOneQuery(t *testing.T) {
	f := newFixture(t)
	if _, err := f.db.UpsertRepo(t.Context(), store.Repo{
		Path:       f.repo.Path + "-other",
		Forge:      store.ForgeGitHub,
		Owner:      testOwnerName,
		Name:       "other-api",
		RemoteURL:  "git@github.com:acme/other-api.git",
		BaseBranch: "main",
	}); err != nil {
		t.Fatalf("register the second repository: %v", err)
	}
	f.plantCursor()

	before, _, _, _ := f.forge.counts()
	if before != 2 {
		t.Fatalf("the settling tick made %d issue listings for 2 repositories, want 2", before)
	}
	commentsBefore := f.forge.repoCommentListings()
	if commentsBefore != 2 {
		t.Fatalf("the settling tick made %d repo comment listings for 2 repositories, want 2", commentsBefore)
	}

	f.issue("Add a /health endpoint")
	f.forge.addIssue(fakeIssue{
		Number:      999,
		Title:       "A pull request wearing an issue's clothes",
		User:        owner(),
		PullRequest: map[string]any{"url": "https://api.github.com/pulls/999"},
	})

	result := f.settle()

	if issues, _, _, _ := f.forge.counts(); issues-before != 2 {
		t.Errorf("the tick made %d issue listings for 2 registered repositories, want 2", issues-before)
	}
	// Discovery is two listings per repository now — the issues that moved and
	// the comments that were written — and still nothing per issue.
	if got := f.forge.repoCommentListings() - commentsBefore; got != 2 {
		t.Errorf("the tick made %d repo comment listings for 2 registered repositories, want 2", got)
	}
	if result.Repos != 2 {
		t.Errorf("polled %d repositories, want 2", result.Repos)
	}
	if result.Issues != 1 {
		t.Errorf("the tick saw %d issues, want 1: the pull request is filtered out", result.Issues)
	}
	if _, err := f.db.GetSessionByTaskRef(t.Context(),
		store.TaskRef(store.ForgeGitHub, testOwnerName, testRepoName, 999)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a task was created for a pull request: %v", err)
	}
}

// TestRunlessCommandsReplyAndAdvanceTheWatermark covers AC-2.18, AC-2.19 and
// AC-2.20 together, because they are one decision: every comment is disposed of
// and the watermark always ends up past it.
func TestRunlessCommandsReplyAndAdvanceTheWatermark(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 81, owner(), "/status")     // known, not yet supported
	f.forge.addComment(testIssue, 82, owner(), "/frobnicate") // not in the vocabulary
	f.forge.addComment(testIssue, 83, owner(), "/spike")      // no text
	f.forge.addComment(testIssue, 84, owner(), "  \n\t ")     // nothing in it

	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns, want none", len(calls))
	}
	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 3 {
		t.Fatalf("posted %d comments, want 3", len(posted))
	}
	for _, body := range posted {
		if !strings.HasPrefix(body, turn.MarkerPrefix) {
			t.Errorf("reply does not carry the marker:\n%s", body)
		}
	}
	if !slices.Equal(reacted, []int64{81, 82, 83}) {
		t.Errorf("reacted to %v, want [81 82 83]: free text is not acknowledged", reacted)
	}
	if got := f.watermark(); got != 84 {
		t.Errorf("watermark is %d, want 84", got)
	}
}

// TestThirdPartyCommentIsRecordedAndTriggersNothing is AC-2.15.
func TestThirdPartyCommentIsRecordedAndTriggersNothing(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 91, stranger(), "/spike do my bidding")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns for a third party, want none", len(calls))
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Errorf("posted %d comments to a third party, want none", len(posted))
	}
	if got := f.watermark(); got != 91 {
		t.Errorf("watermark is %d, want 91", got)
	}

	var recorded store.Comment
	for _, c := range f.comments() {
		if c.ForgeCommentID == 91 {
			recorded = c
		}
	}
	if recorded.Classification != store.ClassificationThirdParty {
		t.Errorf("classified %q, want %q", recorded.Classification, store.ClassificationThirdParty)
	}
	if recorded.Tag == "" {
		t.Error("a third-party comment must be tagged")
	}
}

// unreactiveForge is the real client with its acknowledgement broken.
type unreactiveForge struct {
	labelFixtureForge
	err error
}

func (f unreactiveForge) AddCommentReaction(
	context.Context, github.Repo, int64, github.ReactionContent,
) error {
	return f.err
}

// TestReactionFailureDoesNotAbortTheTurn is the second half of AC-2.17. A
// missing 👀 is a cosmetic loss; refusing the run over one would be a real one.
func TestReactionFailureDoesNotAbortTheTurn(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.daemon = f.newDaemon(daemon.Options{
		Forge: unreactiveForge{labelFixtureForge: f.client, err: errors.New("reactions are disabled on this issue")},
	})
	f.settle()

	f.forge.addComment(testIssue, 101, owner(), "/spike carry on regardless")
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{101}) {
		t.Fatalf("turns ran for %v, want [101]: a failed reaction must not abort the turn", got)
	}
	if got := f.watermark(); got != 101 {
		t.Errorf("watermark is %d, want 101", got)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Errorf("posted %d reports, want 1", len(posted))
	}
	if !strings.Contains(f.logs.String(), "could not acknowledge the comment") {
		t.Error("the failed reaction was not logged")
	}
}

// TestRateLimitBacksOffAndResumes is AC-2.12.
func TestRateLimitBacksOffAndResumes(t *testing.T) {
	f := newFixture(t)
	const retryAfter = 45 * time.Second
	// The fixture's actual primary window expires during this refusal. A
	// Retry-After alone does not authorize spending a still-exhausted window.
	f.forge.quotaReset = f.clock.Now().Add(retryAfter).Unix()
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addComment(testIssue, 111, owner(), "/spike after the limit clears")

	cursorBefore := f.cursor()
	issuesBefore, _, _, _ := f.forge.counts()

	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != issuesBefore+1 {
			return false
		}
		writeRateLimit(w, retryAfter)
		return true
	}

	_, err := f.tickErr()
	if err == nil {
		t.Fatal("a rate-limited tick returned no error")
	}
	if !errors.As(err, new(*github.RateLimitError)) {
		t.Fatalf("error is %v, want a *github.RateLimitError", err)
	}

	// Nothing moved: a rate limit costs time and nothing else.
	cursorAfterLimit := f.cursor()
	if !cursorAfterLimit.TasksUpdatedSince.Equal(cursorBefore.TasksUpdatedSince) {
		t.Errorf("cursor moved to %s during a rate-limited tick; it was %s",
			cursorAfterLimit.TasksUpdatedSince, cursorBefore.TasksUpdatedSince)
	}
	if !cursorAfterLimit.LastTickAt.Equal(cursorBefore.LastTickAt) {
		t.Error("a failed tick recorded itself as a completed tick")
	}
	if got := f.watermark(); got != -1 {
		t.Errorf("watermark is %d; a rate-limited tick must touch nothing", got)
	}

	// The next tick is skipped without going near the network.
	issuesAfterLimit, _, _, _ := f.forge.counts()
	skipped := f.settle()
	if !skipped.Skipped {
		t.Error("the tick after a rate limit was not skipped")
	}
	if issues, _, _, _ := f.forge.counts(); issues != issuesAfterLimit {
		t.Errorf("the skipped tick made %d extra requests, want 0", issues-issuesAfterLimit)
	}
	if skipped.RetryAt.IsZero() {
		t.Error("the skipped tick does not say when polling resumes")
	}

	// Past the backoff, polling resumes on its own with nothing lost.
	f.clock.advance(retryAfter + time.Second)
	f.forge.mu.Lock()
	f.forge.quotaReset = f.clock.Now().Add(time.Hour).Unix()
	f.forge.mu.Unlock()
	resumed := f.settle()
	if resumed.Skipped {
		t.Fatal("the daemon did not resume after the backoff expired")
	}
	// The remote hold ended. Without a usable quota window the token bucket
	// bounds discovery, so polling resumes at once instead of waiting for fresh
	// quota evidence (#793).
	if resumed.Dispatched != 1 {
		t.Errorf("dispatched %d tasks on resuming, want 1", resumed.Dispatched)
	}
	if !strings.Contains(f.logs.String(), "backing off") {
		t.Error("the backoff was not logged")
	}
}

// TestTransportErrorLeavesCursorAndWatermarksUnchanged is AC-2.13.
//
// The drop is on the repository's issue listing, which is discovery — the Tick
// goroutine, before any task has been touched. That is the only kind of failure
// that still ends a tick, and it is deliberately the only kind: nothing has been
// dispatched, no watermark has moved, and the cursor is not persisted, so the
// next tick asks for the same window and redoes the work.
//
// A transport error met *inside* a turn no longer reaches here at all. It ends
// that one worker, leaves the session's backlog flag standing and lets every
// other turn in flight carry on, which is what
// TestAFailedBootstrapLeavesTheBacklogFlagSet pins.
func TestTransportErrorLeavesCursorAndWatermarksUnchanged(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight; the cursor is now persisted

	cursorBefore := f.cursor()
	watermarkBefore := f.watermark()
	f.forge.addComment(testIssue, 121, owner(), "/spike after the network comes back")

	// The third issue listing of this test: one for the cursor-planting tick,
	// one for the first sight, and this one.
	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != 3 {
			return false
		}
		hangUp(t, w)
		return true
	}

	if _, err := f.tickErr(); err == nil {
		t.Fatal("a tick whose connection was dropped returned no error")
	}

	if got := f.cursor(); !got.TasksUpdatedSince.Equal(cursorBefore.TasksUpdatedSince) {
		t.Errorf("cursor moved to %s after a transport error; it was %s",
			got.TasksUpdatedSince, cursorBefore.TasksUpdatedSince)
	}
	if got := f.watermark(); got != watermarkBefore {
		t.Errorf("watermark moved to %d after a transport error; it was %d", got, watermarkBefore)
	}
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns, want none", len(calls))
	}

	// The next tick redoes the work.
	f.forge.onListIssues = nil
	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{121}) {
		t.Errorf("turns ran for %v after recovery, want [121]", got)
	}
	if got := f.watermark(); got != 121 {
		t.Errorf("watermark is %d, want 121", got)
	}
}

// TestCursorIsInitialisedToNowAndSurvivesRestart is AC-2.9.
func TestCursorIsInitialisedToNowAndSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	// An issue that already exists when litefactory first starts. It must not
	// be replayed: the cursor is planted at now, not at the beginning of time.
	f.issue("An issue that predates the daemon")

	firstEver := f.settle()
	if firstEver.Cursor.TasksUpdatedSince.IsZero() {
		t.Fatal("the first-ever tick left the cursor at the beginning of history")
	}
	if firstEver.Cursor.LastTickAt.IsZero() {
		t.Error("the cursor records no last tick")
	}
	if firstEver.Issues != 0 {
		t.Errorf("the first-ever tick listed %d issues, want none: the cursor starts at now", firstEver.Issues)
	}
	if sinceValues := f.forge.since(); len(sinceValues) != 1 || sinceValues[0] == "" {
		t.Errorf("the first listing asked for %v, want a since value of now", sinceValues)
	}
	persisted := f.cursor()

	// A new daemon over the same database is a restart in every way that
	// matters here: it holds no state of its own.
	f.daemon = f.newDaemon(daemon.Options{InstanceID: "daemon-instance-restarted"})
	f.settle()

	sinceValues := f.forge.since()
	if len(sinceValues) != 2 {
		t.Fatalf("recorded %d listings, want 2", len(sinceValues))
	}
	// The persisted cursor, less the overlap: every tick after the first-ever
	// one reaches back before where it left off, so an index that catches up
	// late cannot drop a comment on the floor.
	wanted := persisted.TasksUpdatedSince.Add(-daemon.ListingOverlap).UTC().Format(time.RFC3339)
	if got := sinceValues[1]; got != wanted {
		t.Errorf("the restarted daemon asked for issues since %q, want the persisted cursor less the overlap, %q",
			got, wanted)
	}
}

// TestTheListingWindowOverlapsSoALateIndexCannotLoseAComment is the regression
// for the seven-hour delay on issue #3.
//
// The comment is real and its timestamp is old — which is what a comment the
// forge indexed late looks like from here, once the cursor has already advanced
// past the moment it was written. Without an overlap the window starts after it
// and it is never seen again.
func TestTheListingWindowOverlapsSoALateIndexCannotLoseAComment(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Logging improvements")
	f.settle() // first sight

	// Two minutes before now: comfortably behind the cursor, comfortably
	// inside the overlap.
	late := f.clock.Now().Add(-2 * time.Minute)
	f.forge.addCommentAt(testIssue, 41, owner(), "/spike make the logs readable", late)

	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{41}) {
		t.Fatalf("turns ran for %v, want [41] — the comment predates the cursor and only the "+
			"overlap reaches it\nlogs:\n%s", got, f.logs)
	}
	if got := f.watermark(); got != 41 {
		t.Errorf("watermark is %d, want 41", got)
	}
}

// TestTheFirstEverTickDoesNotReachBack: the overlap must not undo AC-2.9.
// Planting the cursor at now is what stops registering a busy repository from
// replaying its history, and reaching back five minutes on the very first tick
// would start doing exactly that.
func TestTheFirstEverTickDoesNotReachBack(t *testing.T) {
	f := newFixture(t)
	f.issue("An issue that predates the daemon")

	firstEver := f.settle()
	if firstEver.Issues != 0 {
		t.Errorf("the first-ever tick listed %d issues, want none", firstEver.Issues)
	}
	since := f.forge.since()
	if len(since) != 1 {
		t.Fatalf("recorded %d listings, want 1", len(since))
	}
	if got, floor := since[0], firstEver.StartedAt.Add(-time.Second).UTC().Format(time.RFC3339); got < floor {
		t.Errorf("the first listing asked for %q, which reaches back before the tick started (%q)", got, floor)
	}
}

// TestTickCostIsBoundedByTheRepoCount asserts the shape of AC-2.8 directly: one
// issue listing per registered repository, per tick, plus one comment listing
// per issue that moved. Nothing scales with the number of comments.
func TestTickCostIsBoundedByTheRepoCount(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	issuesBefore, commentsBefore, _, _ := f.forge.counts()

	f.issue("Add a /health endpoint")
	for id := int64(131); id <= 135; id++ {
		f.forge.addComment(testIssue, id, stranger(), "chatter")
	}

	f.settle()

	issues, comments, _, _ := f.forge.counts()
	if issues-issuesBefore != 1 {
		t.Errorf("the tick made %d issue listings for one repository, want 1", issues-issuesBefore)
	}
	if comments-commentsBefore != 1 {
		t.Errorf("the tick made %d comment listings for one issue with five comments, want 1",
			comments-commentsBefore)
	}
}

// TestACommentFoundOnlyByTheCommentListingStillRuns is the regression for the
// seven-hour delay on issue #3.
//
// The issue is left visibly stale — its update time sits well before the poll
// cursor, so the issue listing will not return it, exactly as GitHub behaved on
// 2026-08-25. The comment must still be found, because discovery no longer
// depends on an issue's update time moving.
func TestACommentFoundOnlyByTheCommentListingStillRuns(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Logging improvements")
	f.settle() // first sight

	f.forge.addComment(testIssue, 41, owner(), "/spike make the logs readable")
	// Then push the issue's update time back into the past, which is what an
	// issue listing that has not caught up looks like from here. Nothing can
	// now bring this issue back through that listing.
	f.forge.staleIssue(testIssue, f.clock.Now().Add(-24*time.Hour))

	f.settle()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{41}) {
		t.Fatalf("turns ran for %v, want [41] — the comment listing is the only way this could "+
			"be found\nlogs:\n%s", got, f.logs)
	}
	if got := f.watermark(); got != 41 {
		t.Errorf("watermark is %d, want 41", got)
	}
}

// closedListingForge is a forge whose issue listing hands back a closed issue
// however it was asked.
//
// It models what the `state=open` query cannot rule out — an issue closed
// between the listing and the tick that reads it — and, more to the point, it
// is what makes the *issue listing* path assertable at all: with the query
// filter working, no closed issue reaches the daemon that way, and a test that
// relied on the fake forge alone would be asserting the query rather than the
// guard.
type closedListingForge struct {
	labelFixtureForge
	issue github.Issue
}

func (f closedListingForge) ListIssues(context.Context, github.Repo, time.Time) ([]github.Issue, error) {
	return []github.Issue{f.issue}, nil
}

// TestAClosedIssueStartsNothingOnEitherDiscoveryPath is AC-2.22. Closing an
// issue is how litefactory is told to stop working on it, and it has to mean
// that whichever listing found the issue — which is the whole reason the skip
// is in processIssue, where the two paths meet, and not in a query.
//
// "Starts nothing" is asserted in its strong form: no run, no reply, no
// reaction, and no rows. A closed issue that left a session and a watermark
// behind would still be wrong, because registering a repository would then
// mean a task per closed issue anyone ever comments on.
func TestAClosedIssueStartsNothingOnEitherDiscoveryPath(t *testing.T) {
	t.Run("found by the comment listing", func(t *testing.T) {
		f := newFixture(t)
		f.plantCursor()
		f.forge.addIssue(fakeIssue{
			Number: testIssue,
			Title:  "Add a /health endpoint",
			Body:   "The service has no way to say whether it is up.",
			User:   stranger(),
			State:  "closed",
		})
		f.forge.addComment(testIssue, 50, owner(), "/spike do the thing anyway")

		_, commentListingsBefore, _, _ := f.forge.counts()
		result := f.settle()

		// The issue listing asks for open issues and does not return it, so
		// this is the comment listing's find — reached through GetIssue, which
		// never saw that query. It is discovered and then deliberately skipped.
		if result.Issues != 1 {
			t.Fatalf("the tick saw %d issues, want 1: the comment listing is the path under test\nlogs:\n%s",
				result.Issues, f.logs)
		}
		assertClosedIssueWasLeftAlone(t, f, result, commentListingsBefore)
	})

	t.Run("found by the issue listing", func(t *testing.T) {
		f := newFixture(t)
		f.plantCursor()

		closed := github.Issue{
			Number:    testIssue,
			Title:     "Add a /health endpoint",
			Body:      "The service has no way to say whether it is up.",
			State:     "closed",
			Author:    github.User{ID: strangerID, Login: strangerLogin},
			UpdatedAt: f.clock.Now(),
		}
		f.daemon = f.newDaemon(daemon.Options{Forge: closedListingForge{labelFixtureForge: f.client, issue: closed}})

		_, commentListingsBefore, _, _ := f.forge.counts()
		result := f.settle()

		if result.Issues != 1 {
			t.Fatalf("the tick saw %d issues, want 1: the issue listing handed it one\nlogs:\n%s",
				result.Issues, f.logs)
		}
		assertClosedIssueWasLeftAlone(t, f, result, commentListingsBefore)
	})
}

// assertClosedIssueWasLeftAlone is the whole of AC-2.22, asserted the same way
// for both discovery paths because the criterion is the same for both.
func assertClosedIssueWasLeftAlone(
	t *testing.T,
	f *fixture,
	result daemon.TickResult,
	commentListingsBefore int,
) {
	t.Helper()

	if result.Dispatched != 0 {
		t.Errorf("dispatched %d turns for a closed issue, want none", result.Dispatched)
	}
	if result.Deferred != 0 {
		t.Errorf("deferred %d sessions on a closed issue, want none — nothing was left waiting", result.Deferred)
	}
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns on a closed issue, want none", len(calls))
	}
	if posted, reacted, _ := f.forge.snapshot(); len(posted) != 0 || len(reacted) != 0 {
		t.Errorf("posted %d comments and %d reactions on a closed issue, want none",
			len(posted), len(reacted))
	}

	// No session and no watermark: the skip happens before GetOrCreateSession,
	// so a closed issue leaves nothing behind at all.
	if _, err := f.db.GetSessionByTaskRef(t.Context(),
		store.TaskRef(store.ForgeGitHub, testOwnerName, testRepoName, testIssue)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a session exists for a closed issue (err = %v); the skip must come before the store write", err)
	}
	if got := f.watermark(); got != -1 {
		t.Errorf("watermark is %d, want none: a closed issue leaves no watermarks row either", got)
	}

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns on a closed issue, want none", len(calls))
	}
	_, commentListings, posts, reactions := f.forge.counts()
	if posts != 0 {
		t.Errorf("posted %d comments about a closed issue, want none", posts)
	}
	if reactions != 0 {
		t.Errorf("reacted to %d comments on a closed issue, want none", reactions)
	}
	// Not even its comments were listed: the skip is above the bootstrap, so a
	// closed issue costs nothing per tick beyond the listing that found it.
	if commentListings != commentListingsBefore {
		t.Errorf("listed the comments of a closed issue %d times, want none",
			commentListings-commentListingsBefore)
	}
	if !strings.Contains(f.logs.String(), "skipping an issue that is not open") {
		t.Errorf("the skip was not logged; a comment that starts nothing has to say why\nlogs:\n%s", f.logs)
	}
}

// TestLowDiskHoldsDispatchUntilSpaceReturns: below min_free_disk_gb no
// comment run or scheduled run starts, the shortage is logged with free and
// required bytes, and the same owed work starts once space returns.
func TestLowDiskHoldsDispatchUntilSpaceReturns(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Disk guard")
	f.settle() // first sight creates the session and watermark

	var free atomic.Uint64
	free.Store(1 << 30)
	f.daemon = f.newLowDiskDaemon(15<<30, func(path string) (uint64, error) {
		if path != f.paths.Home() {
			t.Errorf("free space read for %q, want LF_HOME %q", path, f.paths.Home())
		}
		return free.Load(), nil
	})
	due := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	if _, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{
		SessionID: f.session().ID, SourceCommentID: 701, OwnerHandle: ownerLogin,
		Payload: "/spike scheduled payload", DueAt: due,
	}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	f.clock.advance(25 * time.Hour)
	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")

	held := f.settle()
	if held.Dispatched != 0 || len(f.turns.calls()) != 0 {
		t.Fatalf("dispatched %d (%d turns) below the free disk threshold\nlogs:\n%s", held.Dispatched, len(f.turns.calls()), f.logs)
	}
	if held.Scheduled != 1 {
		t.Fatalf("scheduled=%d, want the owed schedule still pending", held.Scheduled)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "free disk space below threshold") ||
		!strings.Contains(logs, "free_bytes=1073741824") || !strings.Contains(logs, "required_bytes=16106127360") {
		t.Fatalf("low-disk warning missing free and required bytes\nlogs:\n%s", logs)
	}
	listener := f.healthListener()
	if listener.RunsHeld != "New runs wait: free disk 1.0 GB is below the 15 GB minimum." {
		t.Fatalf("published runs held = %q while the guard holds runs", listener.RunsHeld)
	}
	if strings.Contains(listener.PauseReason, "free disk") {
		t.Fatalf("disk hold under the pause reason: %q", listener.PauseReason)
	}

	free.Store(20 << 30)
	for i := 0; i < 3 && len(f.turns.calls()) < 2; i++ {
		f.settle()
	}
	if held := f.healthListener().RunsHeld; held != "" {
		t.Fatalf("published runs held = %q after space returned", held)
	}
	var texts []string
	for _, c := range f.turns.calls() {
		texts = append(texts, c.Command.Text)
	}
	if !slices.Contains(texts, "add a health endpoint") || !slices.Contains(texts, "scheduled payload") {
		t.Fatalf("owed work after space returned = %q, want the comment and the schedule\nlogs:\n%s", texts, f.logs)
	}
}

// TestLowDiskHoldClearedBySkippedTick: a tick that returns before the disk
// guard runs, here one skipped while backing off from a forge rate limit,
// publishes no stale run hold.
func TestLowDiskHoldClearedBySkippedTick(t *testing.T) {
	f := newFixture(t)
	const retryAfter = 45 * time.Second
	f.plantCursor()
	f.issue("Disk guard")
	f.settle()
	f.daemon = f.newLowDiskDaemon(15<<30, func(string) (uint64, error) { return 1 << 30, nil })
	f.forge.addComment(testIssue, 41, owner(), "/spike held by the disk guard")
	f.settle()
	if held := f.healthListener().RunsHeld; held == "" {
		t.Fatalf("no run hold published below the threshold\nlogs:\n%s", f.logs)
	}

	f.forge.mu.Lock()
	f.forge.quotaReset = f.clock.Now().Add(retryAfter).Unix()
	f.forge.mu.Unlock()
	issuesBefore, _, _, _ := f.forge.counts()
	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != issuesBefore+1 {
			return false
		}
		writeRateLimit(w, retryAfter)
		return true
	}
	if _, err := f.tickErr(); !errors.As(err, new(*github.RateLimitError)) {
		t.Fatalf("rate-limited tick error = %v, want a *github.RateLimitError", err)
	}
	if skipped := f.settle(); !skipped.Skipped {
		t.Fatal("the tick after a rate limit was not skipped")
	}
	if held := f.healthListener().RunsHeld; held != "" {
		t.Fatalf("skipped tick kept a stale run hold %q", held)
	}
}

// healthListener reads the published listener row.
func (f *fixture) healthListener() store.ListenerStatus {
	f.t.Helper()
	snap, err := f.db.ReadHealthSnapshot(f.t.Context())
	if err != nil || !snap.Found {
		f.t.Fatalf("read health snapshot: found=%v err=%v", snap.Found, err)
	}
	return snap.Listener
}

// newLowDiskDaemon rebuilds the fixture daemon with the free-disk guard.
func (f *fixture) newLowDiskDaemon(minFree uint64, read func(string) (uint64, error)) *daemon.Daemon {
	f.t.Helper()
	d, err := daemon.New(daemon.Options{
		DefaultAgent:     agent.Kind(store.AgentClaude),
		ToolProfiles:     config.DefaultToolProfiles().Permissions,
		Agents:           turn.RunnerSet{agent.Kind(store.AgentClaude): nil},
		Classifiers:      map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}},
		DB:               f.db,
		Forge:            f.client,
		Filter:           f.filter,
		Turn:             f.turns,
		Reporter:         f.reporter,
		InstanceID:       "daemon-instance-test",
		Interval:         testInterval,
		Logger:           f.logger,
		Now:              f.clock.Now,
		ShutdownGrace:    500 * time.Millisecond,
		MinFreeDiskBytes: minFree,
		DiskPath:         f.paths.Home(),
		FreeDiskBytes:    read,
	})
	if err != nil {
		f.t.Fatalf("build the daemon: %v", err)
	}
	return d
}

func TestBugIssueBodyStartsAtFirstSight(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		name := "unassigned"
		if assigned {
			name = "assigned"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			// The executor records the request; concrete runtime execution is
			// covered by turn's Bug bootstrap fixture.
			runtime, err := turn.NewGraphBugRuntime(t.Context(), turn.Options{DB: f.db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: f.reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Forge: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) {
				return turn.BugRolePermissions{}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			f.daemon = f.newDaemon(daemon.Options{Bug: runtime})
			f.plantCursor()
			issue := fakeIssue{Number: testIssue, Title: "Bug bootstrap", Body: "Investigate this failure", User: owner(), Labels: []fakeLabel{{Name: "bug"}}}
			if assigned {
				issue.Assignees = []fakeUser{owner()}
			}
			f.forge.addIssue(issue)
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 {
				t.Fatalf("calls=%d logs=%s", len(calls), f.logs)
			}
			req := calls[0]
			if req.Workflow != agent.WorkflowBug || req.Trigger.CommentID != 0 || req.Trigger.AuthorID != ownerID || req.Command.Text != issue.Body || req.Issue.Body != issue.Body {
				t.Fatalf("wrong Bug body request: %+v", req)
			}
			if f.watermark() != 0 {
				t.Fatal("body advanced comment watermark")
			}
			f.settle()
			if len(f.turns.calls()) != 1 {
				t.Fatal("body was replayed")
			}
		})
	}
}

// Successful discovery cannot reset the task's retry deadline, and a healthy
// sibling is still admitted while the failing task is held.
func TestTransientExecutionBackoffPreservesOtherTasks(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Transient failure")
	f.settle()
	fail := true
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if fail && req.Issue.Number == testIssue {
			return turn.Result{}, errors.New("temporary preparation failure")
		}
		return f.ranSuccessfully(ctx, req)
	})
	f.forge.addComment(testIssue, 40, owner(), "/spike run this")
	f.settle()
	if got := len(f.turns.calls()); got != 1 {
		t.Fatalf("attempts=%d", got)
	}
	for range 3 {
		f.settle()
	}
	if got := len(f.turns.calls()); got != 1 {
		t.Fatalf("heartbeat retried before due: %d", got)
	}
	f.clock.advance(testInterval)
	f.settle()
	if got := len(f.turns.calls()); got != 2 {
		t.Fatalf("due attempt missing: %d", got)
	}
	// The second failure doubles the interval.
	f.clock.advance(testInterval)
	f.settle()
	if got := len(f.turns.calls()); got != 2 {
		t.Fatalf("second delay did not grow: %d", got)
	}
	fail = false
	f.clock.advance(testInterval)
	f.settle()
	if got := f.watermark(); got != 40 {
		t.Fatalf("recovery watermark=%d", got)
	}
	// Consumed work resets the delay for the next independent instruction.
	f.forge.addComment(testIssue, 41, owner(), "/spike next")
	f.settle()
	if got := f.watermark(); got != 41 {
		t.Fatalf("progress did not reset: %d", got)
	}
}

func TestTransientMaterialisationAndListingBackoff(t *testing.T) {
	for _, stage := range []string{"materialise", "comments"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issue("Transient read failure")
			f.settle()
			f.clock.advance(pastListing)
			f.settle()
			f.needsPoll(testIssue)
			g := &closureForge{labelFixtureForge: f.client}
			var calls atomic.Int64
			if stage == "materialise" {
				g.after = func(_ context.Context, _ int, _ github.Issue, _ error) (github.Issue, error) {
					calls.Add(1)
					return github.Issue{}, errors.New("temporary lookup failure")
				}
			} else {
				f.forge.mu.Lock()
				f.forge.onListComments = func(w http.ResponseWriter, _ int) bool {
					calls.Add(1)
					http.Error(w, "temporary listing failure", http.StatusBadGateway)
					return true
				}
				f.forge.mu.Unlock()
			}
			f.daemon = f.newDaemon(daemon.Options{Forge: g})
			f.settle()
			first := calls.Load()
			if first == 0 {
				t.Fatal("failure not injected")
			}
			for range 3 {
				f.settle()
			}
			if got := calls.Load(); got != first {
				t.Fatalf("calls before due=%d, want %d", got, first)
			}
			f.clock.advance(testInterval)
			f.settle()
			if got := calls.Load(); got != 2*first {
				t.Fatalf("due calls=%d, want %d", got, 2*first)
			}
			f.clock.advance(testInterval)
			f.settle()
			if got := calls.Load(); got != 2*first {
				t.Fatalf("delay did not double: %d", got)
			}
		})
	}
}

func TestTransientFailureDoesNotHoldHealthySibling(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Failing task")
	f.forge.addIssue(fakeIssue{Number: 124, Title: "Healthy task", User: stranger()})
	f.bootstrapIssues(f.repo, testIssue, 124)
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Issue.Number == testIssue {
			return turn.Result{}, errors.New("temporary preparation failure")
		}
		return f.ranSuccessfully(ctx, req)
	})
	f.forge.addComment(testIssue, 40, owner(), "/spike failing")
	f.settle()
	f.forge.addComment(124, 41, owner(), "/spike healthy")
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{40, 41}) {
		t.Fatalf("healthy sibling blocked or failed task repeated: %v", got)
	}
}

// #1008: a Research owner comment whose saved launch is bound to an instance
// that has since finished is answered once and consumed by the real daemon, and
// the queue moves on to the next comment. Before that, rate limits and plain
// failures on a running instance never retire it (C3, C11), and a failed post
// of the answer leaves the comment pending until a later tick answers it once
// (C4). Without the fix every tick fails with "graph instance already terminal".
func TestFinishedResearchLaunchIsAnsweredAndQueueMovesOn(t *testing.T) {
	const notApplied = "This answer was not applied; please post it again."
	const commentA, commentB = int64(40), int64(41)
	ctx := t.Context()
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Investigate boundaries", "Mode: Research")
	f.settle()
	session := f.sessionOf(testIssue)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// failure selects what the driver does after admission: a transient error
	// or the production workflow driver.
	var failure atomic.Value
	failure.Store("rate")
	var calls atomic.Int64
	var bInstance atomic.Int64
	driver := func(ctx context.Context, opts turn.Options, req turn.Request, result turn.Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (turn.Result, error) {
		if store.IsGraphTrigger(req.Trigger.CommentID) {
			// The owed stage of the parked instance is not under test.
			return result, errors.New("graph step held by the test")
		}
		calls.Add(1)
		if req.Trigger.CommentID == commentB {
			bInstance.Store(admission.Instance.ID)
			return result, errors.New("comment B reached its turn")
		}
		switch failure.Load() {
		case "rate":
			return result, &github.RateLimitError{RetryAfter: time.Second}
		case "plain":
			return result, errors.New("network unreachable")
		}
		return turn.ExecuteResearchWorkflow(ctx, opts, req, result, definition, admission)
	}
	opts, agentCalls := finishedResearchOptions(t, f, driver)
	f.daemon = f.newDaemon(daemon.Options{Turn: daemon.NewExecutor(opts), Agents: opts.Runners})
	notices := func() int {
		posted, _, _ := f.forge.snapshot()
		n := 0
		for _, body := range posted {
			if strings.Contains(body, notApplied) {
				n++
			}
		}
		return n
	}
	tick := func() {
		f.clock.advance(time.Hour)
		f.settle()
	}

	f.forge.addComment(testIssue, commentA, owner(), "/research inspect boundaries")
	for _, kind := range []string{"rate", "rate", "plain", "plain"} {
		failure.Store(kind)
		before := calls.Load()
		tick()
		if calls.Load() == before {
			t.Fatalf("%s tick did not reach the turn: logs=%s", kind, f.logs)
		}
		if f.watermark() >= commentA || notices() != 0 {
			t.Fatalf("transient %s failure retired the comment: watermark=%d notices=%d", kind, f.watermark(), notices())
		}
	}

	instance, err := f.db.LatestResearchInstance(ctx, session.ID)
	check(err)
	if instance.Status != store.GraphRunning {
		t.Fatalf("instance not running after transient failures: %+v", instance)
	}
	check(f.db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphCancelled, time.Time{}, f.clock.Now()))
	failure.Store("real")

	// The answer's post fails: the comment stays pending with the answer
	// retained for recovery, and is not consumed.
	var failedPosts atomic.Int64
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		failedPosts.Add(1)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}
	tick()
	f.forge.onPostComment = nil
	if failedPosts.Load() == 0 || f.watermark() >= commentA || notices() != 0 || agentCalls.Load() != 0 {
		t.Fatalf("failed answer post consumed the comment: failed=%d watermark=%d notices=%d logs=%s", failedPosts.Load(), f.watermark(), notices(), f.logs)
	}
	pending, found, err := f.db.FindPendingReportDelivery(ctx, session.ID, commentA)
	check(err)
	if !found || !strings.Contains(pending.Body, notApplied) {
		t.Fatalf("pending answer lacks the not-applied reason: found=%v body=%q", found, pending.Body)
	}
	// The uncertain post did land; recovery finds it and the comment is
	// consumed without a second answer.
	f.restorePendingReport(commentA)
	if !settleUntilHandled(f, commentA, 4) {
		t.Fatalf("finished launch was never consumed: watermark=%d logs=%s", f.watermark(), f.logs)
	}
	if notices() != 0 || agentCalls.Load() != 0 || strings.Contains(f.logs.String(), "graph instance already terminal") {
		t.Fatalf("finished launch answered more than once: notices=%d agent=%d logs=%s", notices(), agentCalls.Load(), f.logs)
	}

	// The queue moves on: comment B reaches its own turn on a successor.
	f.forge.addComment(testIssue, commentB, owner(), "/research inspect the next boundary")
	tick()
	if bInstance.Load() == 0 || bInstance.Load() == instance.ID {
		t.Fatalf("comment B did not reach a successor instance: got %d (finished %d) logs=%s", bInstance.Load(), instance.ID, f.logs)
	}
	if notices() != 0 {
		t.Fatalf("not-applied answer posted again %d times", notices())
	}
}

// #1008 C7: without a post failure the finished launch is answered with exactly
// one post on its first replayed turn and consumed.
func TestFinishedResearchLaunchIsAnsweredWithOnePost(t *testing.T) {
	const notApplied = "This answer was not applied; please post it again."
	const commentA = int64(40)
	ctx := t.Context()
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Investigate boundaries", "Mode: Research")
	f.settle()
	session := f.sessionOf(testIssue)
	var admitted atomic.Bool
	driver := func(ctx context.Context, opts turn.Options, req turn.Request, result turn.Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (turn.Result, error) {
		if store.IsGraphTrigger(req.Trigger.CommentID) || admitted.Load() {
			return result, errors.New("failed before any run")
		}
		return turn.ExecuteResearchWorkflow(ctx, opts, req, result, definition, admission)
	}
	opts, agentCalls := finishedResearchOptions(t, f, driver)
	f.daemon = f.newDaemon(daemon.Options{Turn: daemon.NewExecutor(opts), Agents: opts.Runners})
	admitted.Store(true)
	f.forge.addComment(testIssue, commentA, owner(), "/research inspect boundaries")
	f.clock.advance(time.Hour)
	f.settle()
	instance, err := f.db.LatestResearchInstance(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphCancelled, time.Time{}, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	admitted.Store(false)
	if !settleUntilHandled(f, commentA, 4) {
		t.Fatalf("finished launch was never consumed: watermark=%d logs=%s", f.watermark(), f.logs)
	}
	posted, _, _ := f.forge.snapshot()
	n := 0
	for _, body := range posted {
		if strings.Contains(body, notApplied) {
			n++
		}
	}
	if n != 1 || agentCalls.Load() != 0 || strings.Contains(f.logs.String(), "graph instance already terminal") {
		t.Fatalf("finished launch not answered once: notices=%d agent=%d logs=%s", n, agentCalls.Load(), f.logs)
	}
}

// finishedResearchOptions wires the real turn executor for a Research task with
// the given workflow driver; the returned counter records agent invocations,
// which a retired comment must never reach.
func finishedResearchOptions(t *testing.T, f *fixture, driver turn.ResearchLaunchDriver) (turn.Options, *atomic.Int64) {
	t.Helper()
	ctx := t.Context()
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(ctx)) })
	agentCalls := &atomic.Int64{}
	runner := retryPlanRunner(func(context.Context, agent.Request) (*agent.RunResult, error) {
		agentCalls.Add(1)
		return nil, errors.New("a retired comment must not reach an agent")
	})
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "finished-research", Logger: f.logger, CloneGate: make(lifecycleCloneGate, 1), Runners: turn.RunnerSet{"claude": runner}, Author: git.Author{Name: "Test", Email: "test@example.invalid"}, ResearchReporter: f.reporter, ResearchDriver: driver, SubIssueCreator: f.client, MCP: &turn.MCPRuntime{Authority: authority, Paths: f.paths, Executable: "/factory/litefactory", Redactor: redact.New(redact.Options{}), Comments: f.client, BotID: botID, Now: f.clock.Now}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		def, err := teamsourcetest.CaptureResearch(ctx, specs)
		if err != nil {
			return nil, err
		}
		configBytes := []byte("research: research/research.dot\n")
		digest := sha256.Sum256(configBytes)
		def.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "research/research.dot", GraphFile: "research.dot", Team: def.Profile.Team, TeamConfig: configBytes, ConfigDigest: hex.EncodeToString(digest[:])}
		return def, nil
	}
	return opts, agentCalls
}

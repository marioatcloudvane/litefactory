package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestFirstSightBootstrapConsumesNothing is AC-2.10. A task litefactory has
// never seen has its watermark planted at the highest comment that already
// exists, and none of them is processed — otherwise registering a repository
// with a long-running issue replays every `/spike` anyone ever wrote on it.
func TestFirstSightBootstrapConsumesNothing(t *testing.T) {
	f := newFixture(t)
	f.settle()
	f.issue("Add a /health endpoint")
	f.forge.addComment(testIssue, 10, owner(), "/spike do the first thing")
	f.forge.addComment(testIssue, 20, stranger(), "any progress?")
	f.forge.addComment(testIssue, 30, owner(), "/spike do the second thing")

	result := f.tick()

	if result.Bootstrapped != 1 {
		t.Errorf("bootstrapped %d tasks, want 1", result.Bootstrapped)
	}
	if result.Runs != 0 || result.Comments != 0 {
		t.Errorf("processed %d comments and %d runs on first sight, want none",
			result.Comments, result.Runs)
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick() // first sight

	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")
	result := f.tick()

	if result.Runs != 1 {
		t.Fatalf("ran %d turns, want 1\nlogs:\n%s", result.Runs, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{40}) {
		t.Errorf("turn triggers %v, want [40]", got)
	}
	if got := f.turns.calls()[0].Command.Text; got != "add a health endpoint" {
		t.Errorf("instruction text is %q", got)
	}

	posted, reacted, order := f.forge.snapshot()
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick() // first sight

	const body = "draft some mockups for the settings screen\n\n- one per state"
	f.forge.addComment(testIssue, 41, owner(), body)
	result := f.tick()

	if result.Runs != 1 {
		t.Fatalf("ran %d turns, want 1\nlogs:\n%s", result.Runs, f.logs)
	}
	if result.Replies != 0 {
		t.Errorf("posted %d replies, want none — free text is answered by the run report", result.Replies)
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")
	f.tick()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("expected one report to feed back, got %d", len(posted))
	}
	// The daemon's own report, arriving as an inbound comment. Its author is
	// the bot's numeric id, and its body carries the marker: either alone is
	// enough for the filter.
	f.forge.addComment(testIssue, 50, bot(), posted[0])

	before := len(f.turns.calls())
	result := f.tick()

	if got := len(f.turns.calls()); got != before {
		t.Errorf("ran %d turns from litefactory's own comment, want none", got-before)
	}
	if result.Runs != 0 {
		t.Errorf("tick reported %d runs, want 0", result.Runs)
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

// TestOwnCommentUnderAHumanAccountIsStillDropped covers the belt-and-braces
// half of the self-filter: the marker catches a report even when the account it
// was posted from is not the one the identities table names.
func TestOwnCommentUnderAHumanAccountIsStillDropped(t *testing.T) {
	f := newFixture(t)
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	f.forge.addComment(testIssue, 41, owner(),
		turn.MarkerPrefix+"status task=github:acme/acme-api#123 run=1 -->\n\nRun 1 succeeded.")
	f.tick()

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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	// Added out of order, as an eventually consistent API might return them.
	f.forge.addComment(testIssue, 70, owner(), "/spike third")
	f.forge.addComment(testIssue, 50, owner(), "/spike first")
	f.forge.addComment(testIssue, 60, owner(), "/spike second")

	f.tick()

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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

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
	result := f.tick()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{11}) {
		t.Fatalf("turns ran for %v; the queue must stop at comment 11, not go on to 12 and 13", got)
	}
	if got := f.watermark(); got != bootstrapped {
		t.Errorf("watermark moved to %d after a failed report; it must stay at %d", got, bootstrapped)
	}
	if result.Deferred != 1 {
		t.Errorf("tick deferred %d tasks, want 1", result.Deferred)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].Status == store.RunReported {
		t.Errorf("runs after the failed report: %+v; the run must stay unreported", runs)
	}

	// The cursor is held back to the issue's update time, so the same comments
	// are listed again rather than being filtered out by `since`.
	f.forge.onPostComment = nil
	f.tick()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{11, 11, 12, 13}) {
		t.Errorf("turns ran for %v, want [11 11 12 13]: 11 retried, then 12 and 13", got)
	}
	if got := f.watermark(); got != 13 {
		t.Errorf("watermark is %d after the retry, want 13", got)
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	f.forge.addComment(testIssue, 21, owner(), "/spike first")
	f.forge.addComment(testIssue, 22, owner(), "/spike second")

	bootstrapped := f.watermark()
	reporter := &scriptedReporter{inner: f.reporter, failOn: 21}
	f.daemon = f.newDaemon(daemon.Options{Reporter: reporter})

	f.tick()

	if got := f.turns.triggers(); !slices.Equal(got, []int64{21}) {
		t.Fatalf("turns ran for %v; comment 22 must not be touched after 21 was not consumed", got)
	}
	if got := f.watermark(); got != bootstrapped {
		t.Errorf("watermark is %d, want %d: nothing may be consumed past an unrecorded report",
			got, bootstrapped)
	}
	if !strings.Contains(f.logs.String(), "posted to the issue but litefactory could not record it") {
		t.Error("the orphaned comment was not logged; it is the one failure that costs a duplicate")
	}
}

// TestLockedTaskLeavesTheCommentUnconsumed is AC-3.1: no queue, no "busy"
// reply, and the comment comes back on a later tick.
func TestLockedTaskLeavesTheCommentUnconsumed(t *testing.T) {
	f := newFixture(t)
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	f.forge.addComment(testIssue, 31, owner(), "/spike while the task is busy")
	f.forge.addComment(testIssue, 32, owner(), "/spike and again")

	bootstrapped := f.watermark()
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		return f.turnResult(ctx, req, turn.DispositionLocked, harness.OutcomeSucceeded)
	})

	f.tick()

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
	f.tick()

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
	f.settle()
	f.issue("Add a /health endpoint")
	f.forge.addIssue(fakeIssue{Number: 124, Title: "Another task", User: stranger()})
	f.tick()

	f.forge.addComment(testIssue, 61, owner(), "/spike on the locked task")
	f.forge.addComment(124, 62, owner(), "/spike on the free task")

	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if req.Trigger.CommentID == 61 {
			return f.turnResult(ctx, req, turn.DispositionLocked, harness.OutcomeSucceeded)
		}
		return f.ranSuccessfully(ctx, req)
	})

	result := f.tick()

	if result.Deferred != 1 {
		t.Errorf("deferred %d tasks, want 1", result.Deferred)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{61, 62}) {
		t.Errorf("turns ran for %v, want [61 62]: the second task is unaffected", got)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Errorf("posted %d comments, want 1 — the free task's report", len(posted))
	}
}

// TestIssueBodyAloneStartsNothing is AC-2.16. Only comments are ever acted on,
// so a task opened by someone else auto-starts nothing — however the body is
// worded.
func TestIssueBodyAloneStartsNothing(t *testing.T) {
	f := newFixture(t)
	f.settle()
	f.forge.addIssue(fakeIssue{
		Number: testIssue,
		Title:  "Please do this",
		Body:   "/spike rewrite the authentication layer",
		User:   owner(),
	})

	f.tick() // first sight
	// Touch the issue so it is listed a second time: a first sight and an
	// ordinary sighting are different code paths, and the body must start
	// nothing on either.
	f.forge.addComment(testIssue, 141, stranger(), "bumping this")
	f.tick()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns from an issue body, want none", len(calls))
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Errorf("posted %d comments in answer to an issue body, want none", len(posted))
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
	f.settle()

	before, _, _, _ := f.forge.counts()
	if before != 2 {
		t.Fatalf("the settling tick made %d issue listings for 2 repositories, want 2", before)
	}

	f.issue("Add a /health endpoint")
	f.forge.addIssue(fakeIssue{
		Number:      999,
		Title:       "A pull request wearing an issue's clothes",
		User:        owner(),
		PullRequest: map[string]any{"url": "https://api.github.com/pulls/999"},
	})

	result := f.tick()

	if issues, _, _, _ := f.forge.counts(); issues-before != 2 {
		t.Errorf("the tick made %d issue listings for 2 registered repositories, want 2", issues-before)
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	f.forge.addComment(testIssue, 81, owner(), "/plan the work") // known, not yet supported
	f.forge.addComment(testIssue, 82, owner(), "/frobnicate")    // not in the vocabulary
	f.forge.addComment(testIssue, 83, owner(), "/spike")         // no text
	f.forge.addComment(testIssue, 84, owner(), "  \n\t ")        // nothing in it

	result := f.tick()

	if result.Runs != 0 {
		t.Errorf("ran %d turns, want none", result.Runs)
	}
	if result.Replies != 3 {
		t.Errorf("posted %d replies, want 3 — an empty comment draws none", result.Replies)
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick()

	f.forge.addComment(testIssue, 91, stranger(), "/spike do my bidding")
	f.tick()

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
	daemon.Forge
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
	f.settle()
	f.issue("Add a /health endpoint")
	f.daemon = f.newDaemon(daemon.Options{
		Forge: unreactiveForge{Forge: f.client, err: errors.New("reactions are disabled on this issue")},
	})
	f.tick()

	f.forge.addComment(testIssue, 101, owner(), "/spike carry on regardless")
	f.tick()

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
	f.settle()
	f.issue("Add a /health endpoint")
	f.forge.addComment(testIssue, 111, owner(), "/spike after the limit clears")

	cursorBefore := f.cursor()
	issuesBefore, _, _, _ := f.forge.counts()

	const retryAfter = 45 * time.Second
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
	skipped := f.tick()
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
	resumed := f.tick()
	if resumed.Skipped {
		t.Fatal("the daemon did not resume after the backoff expired")
	}
	if resumed.Bootstrapped != 1 {
		t.Errorf("bootstrapped %d tasks on resuming, want 1", resumed.Bootstrapped)
	}
	if !strings.Contains(f.logs.String(), "backing off") {
		t.Error("the backoff was not logged")
	}
}

// TestTransportErrorLeavesCursorAndWatermarksUnchanged is AC-2.13.
func TestTransportErrorLeavesCursorAndWatermarksUnchanged(t *testing.T) {
	f := newFixture(t)
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick() // first sight; the cursor is now persisted

	cursorBefore := f.cursor()
	watermarkBefore := f.watermark()
	f.forge.addComment(testIssue, 121, owner(), "/spike after the network comes back")

	f.forge.onListComments = func(w http.ResponseWriter, call int) bool {
		if call != 2 {
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
	f.forge.onListComments = nil
	f.tick()

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

	firstEver := f.tick()
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
	f.tick()

	sinceValues := f.forge.since()
	if len(sinceValues) != 2 {
		t.Fatalf("recorded %d listings, want 2", len(sinceValues))
	}
	wanted := persisted.TasksUpdatedSince.UTC().Format(time.RFC3339)
	if got := sinceValues[1]; got != wanted {
		t.Errorf("the restarted daemon asked for issues since %q, want the persisted %q", got, wanted)
	}
}

// TestTickCostIsBoundedByTheRepoCount asserts the shape of AC-2.8 directly: one
// issue listing per registered repository, per tick, plus one comment listing
// per issue that moved. Nothing scales with the number of comments.
func TestTickCostIsBoundedByTheRepoCount(t *testing.T) {
	f := newFixture(t)
	f.settle()
	issuesBefore, commentsBefore, _, _ := f.forge.counts()

	f.issue("Add a /health endpoint")
	for id := int64(131); id <= 135; id++ {
		f.forge.addComment(testIssue, id, stranger(), "chatter")
	}

	f.tick()

	issues, comments, _, _ := f.forge.counts()
	if issues-issuesBefore != 1 {
		t.Errorf("the tick made %d issue listings for one repository, want 1", issues-issuesBefore)
	}
	if comments-commentsBefore != 1 {
		t.Errorf("the tick made %d comment listings for one issue with five comments, want 1",
			comments-commentsBefore)
	}
}

package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// testBotToken is the bot credential every test here posts with. It is
// token-shaped so that the redactor treats it as one, and it is the only
// credential the forge is ever allowed to see (AC-4.11).
const testBotToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyz"

// TestReportRunPostsThenCommitsAllThree is AC-4.10 in its ordinary form: one
// comment, and the three writes that record it landing together after it.
func TestReportRunPostsThenCommitsAllThree(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}

	post := forge.only(t)
	switch {
	case posted.Skipped:
		t.Error("Posted.Skipped = true, want the report to have been posted")
	case posted.CommentID != post.id:
		t.Errorf("Posted.CommentID = %d, want the forge's %d", posted.CommentID, post.id)
	case post.path != "/repos/acme/acme-api/issues/123/comments":
		t.Errorf("posted to %q, want the issue the task ref names", post.path)
	case !strings.HasPrefix(post.body, report.StatusMarker(f.ref(), 1)):
		t.Errorf("body does not begin with the run marker (AC-4.2):\n%s", post.body)
	}

	// The three writes of AC-4.10.
	run := f.runs()[0]
	switch {
	case run.Status != store.RunReported:
		t.Errorf("run status = %q, want %q", run.Status, store.RunReported)
	case run.ResultCommentID != post.id:
		t.Errorf("result_comment_id = %d, want %d", run.ResultCommentID, post.id)
	case posted.Run.Status != store.RunReported:
		t.Errorf("Posted.Run.Status = %q, want %q", posted.Run.Status, store.RunReported)
	}
	if wm := watermark(t, f); wm != testComment {
		t.Errorf("watermark = %d, want %d: the report consumes its triggering comment", wm, testComment)
	}
}

// TestReportPostFailureLeavesTheWatermarkAndTheRunUntouched is the criterion the
// whole ordering exists for (AC-4.10).
//
// The retry half matters as much as the invariance half: a post that failed must
// cost one HTTP request, not one more run of claude.
func TestReportPostFailureLeavesTheWatermarkAndTheRunUntouched(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	forge.failWith(http.StatusInternalServerError)

	if _, err := reporter.ReportTurn(t.Context(), result); err == nil {
		t.Fatal("ReportTurn = nil error, want the forge failure")
	}

	// Both halves of "untouched", asserted from the database rather than from
	// the returned value.
	run := f.runs()[0]
	switch {
	case run.Status != store.RunSucceeded:
		t.Errorf("run status after a failed post = %q, want it left at %q", run.Status, store.RunSucceeded)
	case run.ResultCommentID != 0:
		t.Errorf("result_comment_id after a failed post = %d, want 0", run.ResultCommentID)
	}
	if _, exists, err := f.db.GetWatermark(t.Context(), f.session.ID); err != nil {
		t.Fatalf("read watermark: %v", err)
	} else if exists {
		t.Error("the watermark moved after a failed post, so the comment would never be reported again")
	}

	// The next tick sees the comment again. It must retry the post and nothing
	// else: the run row still exists, so claude is not re-run (AC-3.4).
	forge.recover()
	retry := f.execute(f.request())
	if retry.Disposition != DispositionAlreadyRan {
		t.Fatalf("disposition on retry = %s, want %s", retry.Disposition, DispositionAlreadyRan)
	}
	if runner.callCount() != 1 {
		t.Errorf("claude ran %d times, want 1: a failed post must never cost a second run", runner.callCount())
	}

	posted, err := reporter.ReportTurn(t.Context(), retry)
	if err != nil {
		t.Fatalf("ReportTurn on retry: %v\nlogs:\n%s", err, f.logs)
	}
	if attempts := forge.attemptCount(); attempts != 2 {
		t.Errorf("forge saw %d requests, want 2: the failed post and the retry", attempts)
	}
	if posts := forge.all(); len(posts) != 1 {
		t.Fatalf("forge accepted %d comments, want exactly 1 (AC-4.1)", len(posts))
	}
	if posted.Run.Status != store.RunReported {
		t.Errorf("run status after the retry = %q, want %q", posted.Run.Status, store.RunReported)
	}
	if wm := watermark(t, f); wm != testComment {
		t.Errorf("watermark after the retry = %d, want %d", wm, testComment)
	}
}

// TestReportRenderFailurePostsNothingAndAdvancesNothing is AC-4.9. A redaction
// pass that panicked leaves a body that must be assumed to carry a credential,
// so the comment is abandoned rather than posted in some safer-looking form.
func TestReportRenderFailurePostsNothingAndAdvancesNothing(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	failed := &redact.PanicError{Value: "index out of range", Type: "runtime.Error", Stack: "goroutine 1"}
	reporter := newReporter(t, f, forge, stubRenderer{err: fmt.Errorf("report: redact comment body: %w", failed)})

	result := f.execute(f.request())
	_, err := reporter.ReportTurn(t.Context(), result)

	if err == nil {
		t.Fatal("ReportTurn = nil error, want the fail-closed redaction error")
	}
	if !strings.Contains(err.Error(), "index out of range") {
		t.Errorf("error = %v, want it to carry the redaction failure", err)
	}
	if posts := forge.all(); len(posts) != 0 {
		t.Fatalf("forge saw %d posts, want none: the body never passed redaction", len(posts))
	}
	if run := f.runs()[0]; run.Status == store.RunReported {
		t.Error("run is reported, want it left un-reported")
	}
	if _, exists, err := f.db.GetWatermark(t.Context(), f.session.ID); err != nil {
		t.Fatalf("read watermark: %v", err)
	} else if exists {
		t.Error("the watermark advanced without a comment being posted")
	}
	// AC-4.9 asks for a loud line, because an unreported run is otherwise
	// indistinguishable from a run nobody looked at.
	if !strings.Contains(f.logs.String(), "level=ERROR") ||
		!strings.Contains(f.logs.String(), "could not render the run report") {
		t.Errorf("no loud log line explains the unreported run:\n%s", f.logs)
	}
}

// TestReportUsesTheBotCredentialOnly is AC-4.11 and the "gh is init-only" review
// invariant, asserted rather than asserted-in-prose.
func TestReportUsesTheBotCredentialOnly(t *testing.T) {
	// Not parallel: it rewrites PATH for this process.
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		result := succeeded(req, f.paths.Home())
		// A credential that reached run output must not reach the comment
		// (AC-4.6). The local JSONL keeps it verbatim; this is the outbound copy.
		result.ResultText = "Added the endpoint. The token was " + testBotToken + "."
		return result, nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	tripwire := ghTripwire(t)

	result := f.execute(f.request())
	if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}

	post := forge.only(t)
	if post.auth != "Bearer "+testBotToken {
		t.Errorf("Authorization = %q, want the bot token: posting is never Mario's identity", post.auth)
	}
	if _, err := os.Stat(tripwire); err == nil {
		t.Error("gh was invoked while reporting; it is confined to internal/identity and `init`")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat the gh tripwire: %v", err)
	}
	if strings.Contains(post.body, testBotToken) {
		t.Errorf("the bot token survived into the outbound body:\n%s", post.body)
	}
	if !strings.Contains(post.body, redact.Replacement) {
		t.Errorf("body carries no %s marker, so redaction did not run on it:\n%s", redact.Replacement, post.body)
	}
}

// TestReportReplyAdvancesWithoutARunRow is the run-less path of AC-2.18 and
// AC-2.20: a comment litefactory will not act on is still a comment it has dealt
// with, and the transaction shape differs because there is no run to close out.
func TestReportReplyAdvancesWithoutARunRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "unsupported command", body: "/plan the migration", want: "does not implement it yet"},
		{name: "unknown command", body: "/frobnicate", want: "do not recognise"},
		{name: "empty spike", body: "/spike", want: "needs something to work on"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				t.Error("claude ran for a comment that spawns nothing")
				return nil, nil
			}))
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)

			const commentID = int64(9100)
			posted, err := reporter.ReportReply(t.Context(), ReplyRequest{
				Ref:       f.ref(),
				SessionID: f.session.ID,
				CommentID: commentID,
				Command:   ParseCommand(tc.body),
			})
			if err != nil {
				t.Fatalf("ReportReply: %v\nlogs:\n%s", err, f.logs)
			}

			post := forge.only(t)
			switch {
			case posted.CommentID != post.id:
				t.Errorf("Posted.CommentID = %d, want %d", posted.CommentID, post.id)
			case !strings.HasPrefix(post.body, report.ReplyMarker(f.ref())):
				t.Errorf("body does not begin with the reply marker (AC-4.2):\n%s", post.body)
			case strings.Contains(post.body, "run="):
				t.Errorf("reply carries a run attribute, and no run happened:\n%s", post.body)
			case !strings.Contains(post.body, tc.want):
				t.Errorf("body does not say %q:\n%s", tc.want, post.body)
			}
			if posted.Run.ID != 0 {
				t.Errorf("Posted.Run.ID = %d, want 0: a reply closes out no run", posted.Run.ID)
			}
			if runs := f.runs(); len(runs) != 0 {
				t.Errorf("allocated %d runs for a reply, want 0", len(runs))
			}
			if wm := watermark(t, f); wm != commentID {
				t.Errorf("watermark = %d, want %d", wm, commentID)
			}
		})
	}
}

// TestReportFailedRunStillPostsAndAdvances is S-9: silence is not an outcome,
// and the failure comment consumes its comment exactly as a success does. No
// automatic retry happens at M0, which is what the advanced watermark means.
func TestReportFailedRunStillPostsAndAdvances(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		result := succeeded(req, f.paths.Home())
		result.Outcome = harness.OutcomeFailed
		result.ExitCode = 2
		result.Reason = "claude exited with status 2"
		result.Stderr = "panic: the model gave up\n"
		return result, nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}

	post := forge.only(t)
	logPath := f.runs()[0].LogPath
	switch {
	case !strings.Contains(post.body, "failed"):
		t.Errorf("failure report is not distinguishable at a glance (AC-4.5):\n%s", post.body)
	case !strings.Contains(post.body, logPath):
		t.Errorf("failure report does not name the local log %s (AC-4.5):\n%s", logPath, post.body)
	case posted.Run.Status != store.RunReported:
		t.Errorf("run status = %q, want %q", posted.Run.Status, store.RunReported)
	}
	if wm := watermark(t, f); wm != testComment {
		t.Errorf("watermark = %d, want %d: a failure comment advances it too (S-9)", wm, testComment)
	}
}

// TestReportPrepareFailurePostsWithNoRunRow is the other half of AC-4.1: a turn
// that never reached claude allocated no run, and still owes the issue a
// comment. The watermark advances on its own, because there is no run row to
// carry it.
func TestReportPrepareFailurePostsWithNoRunRow(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran after preparation failed")
		return nil, nil
	}))
	f.repo.SetOriginURL(filepath.Join(filepath.Dir(f.repo.Dir), "there-is-no-such-repository.git"))
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	if result.Disposition != DispositionPrepareFailed {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionPrepareFailed)
	}
	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}

	post := forge.only(t)
	switch {
	case !strings.HasPrefix(post.body, report.StatusMarker(f.ref(), 1)):
		t.Errorf("body does not carry the marker of the run that would have been:\n%s", post.body)
	case !strings.Contains(post.body, "fetch"):
		t.Errorf("body does not say what could not be done:\n%s", post.body)
	case posted.Run.ID != 0:
		t.Errorf("Posted.Run.ID = %d, want 0: no run row was ever allocated", posted.Run.ID)
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs, want 0", len(runs))
	}
	if wm := watermark(t, f); wm != testComment {
		t.Errorf("watermark = %d, want %d", wm, testComment)
	}
}

// TestReportReplayOfAReportedRunIsANoOp is AC-3.4 seen from the reporting side.
// Crash replay must not produce a second comment for a run that already has one.
func TestReportReplayOfAReportedRunIsANoOp(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	first, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}

	// The comment comes round again: the daemon replays it, and the same report
	// is offered twice — once through a fresh turn, once straight to ReportRun.
	replay := f.execute(f.request())
	if replay.Disposition != DispositionAlreadyReported {
		t.Fatalf("disposition on replay = %s, want %s", replay.Disposition, DispositionAlreadyReported)
	}
	viaTurn, err := reporter.ReportTurn(t.Context(), replay)
	if err != nil {
		t.Fatalf("ReportTurn on a replay: %v", err)
	}
	viaRun, err := reporter.ReportRun(t.Context(), RunReport{
		Run:       report.Run{Ref: f.ref(), Ordinal: 1},
		SessionID: f.session.ID,
		CommentID: testComment,
		RunID:     first.Run.ID,
	})
	if err != nil {
		t.Fatalf("ReportRun on a reported run: %v", err)
	}

	if !viaTurn.Skipped || !viaRun.Skipped {
		t.Errorf("Skipped = %t/%t, want both true", viaTurn.Skipped, viaRun.Skipped)
	}
	if posts := forge.all(); len(posts) != 1 {
		t.Errorf("forge saw %d posts, want the original 1", len(posts))
	}
	if viaRun.Run.ResultCommentID != first.CommentID {
		t.Errorf("result_comment_id = %d, want the original %d", viaRun.Run.ResultCommentID, first.CommentID)
	}
}

// TestReportRunTriggeredByTheIssueBody is the turn half of AC-2.23.
//
// A run started by the issue body carries trigger id 0, so the watermark
// advance that closes its report is MAX(0, 0): the watermark stays where
// bootstrap planted it and every comment the issue later receives still runs.
// The report itself is posted like any other run's (AC-4.1).
func TestReportRunTriggeredByTheIssueBody(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	// First sight of an issue with no comments plants the watermark at 0 and
	// only then runs the body (AC-2.10b).
	if _, _, err := f.db.BootstrapWatermark(t.Context(), f.session.ID, 0); err != nil {
		t.Fatalf("bootstrap the watermark: %v", err)
	}

	body := "/spike add a health endpoint that reports the build sha"
	req := f.request()
	req.Issue.Body = body
	req.Command = ParseCommand(body)
	req.Trigger = Trigger{CommentID: 0, Body: "", Author: req.Issue.Author}

	result := f.execute(req)
	if result.Disposition != DispositionRan {
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionRan, f.logs)
	}
	if result.TriggerCommentID != 0 {
		t.Errorf("TriggerCommentID = %d, want 0: the body is not a comment", result.TriggerCommentID)
	}

	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}
	if posted.Skipped {
		t.Error("Skipped = true, want a body-triggered run to report like any other")
	}
	if posts := forge.all(); len(posts) != 1 {
		t.Fatalf("forge saw %d posts, want 1", len(posts))
	}
	if posted.Run.TriggerCommentID != 0 {
		t.Errorf("runs row trigger_comment_id = %d, want 0", posted.Run.TriggerCommentID)
	}
	if got := watermark(t, f); got != 0 {
		t.Errorf("watermark after a body run = %d, want it still at 0: a positive id here "+
			"would swallow every comment the issue ever receives", got)
	}
}

// TestReportSkipsALockedTurn: a comment left for a later tick must not be
// consumed, or "no queue" would mean "dropped" (AC-3.1).
func TestReportSkipsALockedTurn(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran for a locked task")
		return nil, nil
	}))
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	if _, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, testOtherOwner); err != nil {
		t.Fatalf("take the lock as another instance: %v", err)
	}

	result := f.execute(f.request())
	if result.Disposition != DispositionLocked {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionLocked)
	}
	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v", err)
	}

	if !posted.Skipped {
		t.Error("Skipped = false, want a locked turn to report nothing")
	}
	if posts := forge.all(); len(posts) != 0 {
		t.Errorf("forge saw %d posts for a locked turn, want 0", len(posts))
	}
	if _, exists, err := f.db.GetWatermark(t.Context(), f.session.ID); err != nil {
		t.Fatalf("read watermark: %v", err)
	} else if exists {
		t.Error("a locked turn consumed its comment; a later tick would never see it again")
	}
}

// TestReportConsumeAdvancesWithoutPosting is AC-2.19: free text is recorded and
// ignored, and ignoring it means consuming it.
func TestReportConsumeAdvancesWithoutPosting(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	const commentID = int64(9200)
	wm, err := reporter.Consume(t.Context(), f.session.ID, commentID)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	if wm.LastCommentID != commentID {
		t.Errorf("watermark = %d, want %d", wm.LastCommentID, commentID)
	}
	if posts := forge.all(); len(posts) != 0 {
		t.Errorf("forge saw %d posts, want none: free text draws no reply", len(posts))
	}
	if got := watermark(t, f); got != commentID {
		t.Errorf("stored watermark = %d, want %d", got, commentID)
	}
}

// TestReportValidatesItsArguments keeps the invalid shapes out of the post path,
// where a bad ref would mean a comment on the wrong issue.
func TestReportValidatesItsArguments(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	ref := f.ref()

	t.Run("options", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			opts ReporterOptions
		}{
			{"no database", ReporterOptions{Forge: forge.client, Renderer: stubRenderer{}, Logger: discardLogger()}},
			{"no forge", ReporterOptions{DB: f.db, Renderer: stubRenderer{}, Logger: discardLogger()}},
			{"no renderer", ReporterOptions{DB: f.db, Forge: forge.client, Logger: discardLogger()}},
			{"no logger", ReporterOptions{DB: f.db, Forge: forge.client, Renderer: stubRenderer{}}},
		}
		for _, tc := range tests {
			if _, err := NewReporter(tc.opts); err == nil {
				t.Errorf("NewReporter(%s) = nil error, want a refusal", tc.name)
			}
		}
	})

	t.Run("run reports", func(t *testing.T) {
		t.Parallel()

		good := RunReport{Run: report.Run{Ref: ref, Ordinal: 1}, SessionID: f.session.ID, CommentID: testComment}
		tests := []struct {
			name   string
			mutate func(*RunReport)
		}{
			{"no session", func(r *RunReport) { r.SessionID = 0 }},
			// 0 is not in this table: it is the issue body, and a body-triggered
			// run owes the issue a report like any other (AC-2.16b).
			{"negative comment id", func(r *RunReport) { r.CommentID = -1 }},
			{"negative run id", func(r *RunReport) { r.RunID = -1 }},
			{"no ref", func(r *RunReport) { r.Run.Ref = f.ref(); r.Run.Ref.Owner = "" }},
			{"zero ordinal", func(r *RunReport) { r.Run.Ordinal = 0 }},
		}
		for _, tc := range tests {
			rep := good
			tc.mutate(&rep)
			if _, err := reporter.ReportRun(t.Context(), rep); err == nil {
				t.Errorf("ReportRun(%s) = nil error, want a refusal", tc.name)
			}
		}
	})

	t.Run("replies", func(t *testing.T) {
		t.Parallel()

		good := ReplyRequest{Ref: ref, SessionID: f.session.ID, CommentID: 9300, Command: ParseCommand("/plan")}
		tests := []struct {
			name   string
			mutate func(*ReplyRequest)
		}{
			{"no session", func(r *ReplyRequest) { r.SessionID = 0 }},
			{"no comment", func(r *ReplyRequest) { r.CommentID = 0 }},
			{"free text", func(r *ReplyRequest) { r.Command = ParseCommand("just talking") }},
			{"a command that spawns a run", func(r *ReplyRequest) { r.Command = ParseCommand("/spike do it") }},
			{"no ref", func(r *ReplyRequest) { r.Ref.Repo = "" }},
		}
		for _, tc := range tests {
			req := good
			tc.mutate(&req)
			if _, err := reporter.ReportReply(t.Context(), req); err == nil {
				t.Errorf("ReportReply(%s) = nil error, want a refusal", tc.name)
			}
		}
	})

	t.Run("consume", func(t *testing.T) {
		t.Parallel()

		if _, err := reporter.Consume(t.Context(), 0, 1); err == nil {
			t.Error("Consume without a session = nil error, want a refusal")
		}
		if _, err := reporter.Consume(t.Context(), f.session.ID, 0); err == nil {
			t.Error("Consume without a comment = nil error, want a refusal")
		}
	})

	if posts := forge.all(); len(posts) != 0 {
		t.Errorf("forge saw %d posts from refused arguments, want 0", len(posts))
	}
}

// newReporter wires the real renderer over the real redactor onto the fake
// forge. Passing a renderer overrides the real one, which is how the fail-closed
// path is reached — a real redaction pass cannot be made to panic from outside
// internal/redact, and that is by design.
func newReporter(t *testing.T, f *fixture, forge *fakeForge, renderer Renderer) *Reporter {
	t.Helper()

	if renderer == nil {
		bodies, err := report.New(report.Options{
			Redactor: redact.New(redact.Options{
				Token:   secrets.NewToken(testBotToken),
				Environ: []string{},
				Logger:  f.opts.Logger,
			}),
			Limit:  github.CommentSizeLimit,
			Logger: f.opts.Logger,
		})
		if err != nil {
			t.Fatalf("build the renderer: %v", err)
		}
		renderer = bodies
	}

	reporter, err := NewReporter(ReporterOptions{
		DB:       f.db,
		Forge:    forge.client,
		Renderer: renderer,
		Logger:   f.opts.Logger,
	})
	if err != nil {
		t.Fatalf("build the reporter: %v", err)
	}
	return reporter
}

// watermark reads how far through the task's comments litefactory has got, and
// is 0 when it has consumed nothing.
func watermark(t *testing.T, f *fixture) int64 {
	t.Helper()

	wm, _, err := f.db.GetWatermark(t.Context(), f.session.ID)
	if err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	return wm.LastCommentID
}

// forgePost is one request the fake forge received.
type forgePost struct {
	path string
	auth string
	body string
	id   int64
}

// fakeForge is GitHub, over real HTTP, behind a real *github.Client.
//
// The client is the production one on purpose: the guarantee under test is that
// the report goes out over the bot credential and nothing else, and a hand-built
// double of the client would be asserting against itself.
type fakeForge struct {
	server *httptest.Server
	client *github.Client

	mu       sync.Mutex
	posts    []forgePost
	attempts int
	status   int
	nextID   int64
}

func newFakeForge(t *testing.T) *fakeForge {
	t.Helper()

	forge := &fakeForge{nextID: 5000}
	forge.server = httptest.NewServer(http.HandlerFunc(forge.handle))
	t.Cleanup(forge.server.Close)

	client, err := github.New(github.Options{
		APIBase:    forge.server.URL,
		Token:      secrets.NewToken(testBotToken),
		HTTPClient: forge.server.Client(),
	})
	if err != nil {
		t.Fatalf("build the github client: %v", err)
	}
	forge.client = client
	return forge
}

func (f *fakeForge) handle(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.nextID++
	f.attempts++
	post := forgePost{
		path: r.URL.Path,
		auth: r.Header.Get("Authorization"),
		body: payload.Body,
		id:   f.nextID,
	}
	status := f.status
	if status == 0 {
		f.posts = append(f.posts, post)
	}
	f.mu.Unlock()

	if status != 0 {
		http.Error(w, `{"message":"the forge is unwell"}`, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"id":%d,"body":%s,"user":{"id":77,"login":"litefactory-bot","type":"User"},`+
		`"html_url":"https://github.test/c/%d","created_at":"2026-08-23T10:00:00Z",`+
		`"updated_at":"2026-08-23T10:00:00Z"}`,
		post.id, strconv.Quote(payload.Body), post.id)
}

// failWith makes every subsequent post fail with status. The request is still
// received — a post that failed is a post that was attempted — but it is not
// recorded as a comment, because none was created.
func (f *fakeForge) failWith(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// recover puts the forge back in working order.
func (f *fakeForge) recover() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = 0
}

// all returns the comments the forge accepted.
func (f *fakeForge) all() []forgePost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forgePost(nil), f.posts...)
}

// attemptCount returns how many requests the forge received, accepted or not.
// It is the difference between "posted once" and "tried twice and posted once".
func (f *fakeForge) attemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

// only returns the single comment the forge accepted, failing the test if there
// was not exactly one. Exactly one is what AC-4.1 asks for.
func (f *fakeForge) only(t *testing.T) forgePost {
	t.Helper()

	posts := f.all()
	if len(posts) != 1 {
		t.Fatalf("forge accepted %d comments, want exactly 1 (AC-4.1)", len(posts))
	}
	return posts[0]
}

// stubRenderer stands in for internal/report when a test needs rendering to
// fail. Its zero value renders nothing successfully, which is enough for the
// argument-validation tests that never reach it.
type stubRenderer struct{ err error }

func (s stubRenderer) RenderRun(ctx context.Context, _ report.Run) (redact.Body, error) {
	return s.body(ctx)
}

func (s stubRenderer) RenderReply(ctx context.Context, _ report.Reply) (redact.Body, error) {
	return s.body(ctx)
}

func (s stubRenderer) body(ctx context.Context) (redact.Body, error) {
	if s.err != nil {
		return redact.Body{}, s.err
	}
	return redact.New(redact.Options{Environ: []string{}}).Apply(ctx, "stub body", github.CommentSizeLimit)
}

// ghTripwire puts a `gh` on PATH that records having been run, and returns the
// path of the file it would write. Nothing in the reporting path may execute it:
// `gh` is Mario's credential, confined to internal/identity and reached only by
// `init` (AC-4.11).
func ghTripwire(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the tripwire is a shell script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "gh-was-invoked")
	script := "#!/bin/sh\ntouch " + strconv.Quote(marker) + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil { //nolint:gosec // a test fixture that must be executable.
		t.Fatalf("write the gh tripwire: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

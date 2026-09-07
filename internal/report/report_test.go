package report_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// todoWriteResolved and todoWriteRequested are the measured finding this story
// exists to make visible ("Settled before planning", IMPLEMENTATION_PLAN.md):
// eight tools requested, seven resolved, TodoWrite silently dropped with no
// error raised anywhere. The resolved order is copied verbatim from a real
// init event.
var (
	todoWriteResolved  = []string{"Task", "Bash", "Edit", "Glob", "Grep", "Read", "Write"}
	todoWriteRequested = append(append([]string{}, todoWriteResolved...), "TodoWrite")
)

// update rewrites the golden files instead of comparing against them.
//
// The goldens are the point of this file: a comment body is prose plus markup,
// and the only useful review of it is a human reading the whole thing and
// deciding whether it says what happened. A diff on a rendered body is that
// review, in a form that survives into every later change.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// testRef is the task every fixture reports against.
var testRef = naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}

const testLogPath = "/Users/mario/.litefactory/logs/runs/github/acme/acme-api/123/0002.jsonl"

// testRunDay is when every fixture run started. It is the date component of the
// session directory the brief link is derived from, and it is fixed and in UTC
// so that the goldens do not move with the calendar or with the timezone of
// whatever machine runs the tests.
var testRunDay = time.Date(2026, 5, 20, 9, 14, 3, 0, time.UTC)

// testBriefPath is where successRun's brief was found: the branch with its
// slashes hyphenated and lowercased, the day that session directory was named
// on, and the feature slug frozen into the branch. The report is handed this
// path rather than computing it — see report.Run.BriefPath — so a fixture that
// leaves it empty is a run with no brief, and renders no link.
//
// Note that the date here is not required to be the run's own day, and that is
// the property worth keeping: a task answered days after its session directory
// was created still links the brief that exists.
const testBriefPath = ".aiteam/lf-123-add-dry-run-to-task-clean/2026-05-20/" +
	"add-dry-run-to-task-clean/RESEARCH_BRIEF.md"

// newReporter builds a Reporter at GitHub's real comment limit, with a
// redactor that scans nothing it was not given: the environment of whatever
// machine runs the tests must not change what a body renders as.
func newReporter(t *testing.T, opts ...func(*redact.Options)) *report.Reporter {
	t.Helper()

	ropts := redact.Options{Environ: []string{}}
	for _, opt := range opts {
		opt(&ropts)
	}
	rep, err := report.New(report.Options{
		Redactor: redact.New(ropts),
		Limit:    github.CommentSizeLimit,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	return rep
}

// successRun is the fixture a passing run produces.
func successRun() report.Run {
	return report.Run{
		Ref:     testRef,
		Ordinal: 2,
		Result: &harness.RunResult{
			Outcome:    harness.OutcomeSucceeded,
			ExitCode:   0,
			SessionID:  "8f1a0c2e-0000-4000-8000-000000000001",
			LogPath:    testLogPath,
			Model:      "claude-sonnet-4-5",
			ResultText: "Added a `--dry-run` flag to `litefactory task clean` and a test that asserts it removes nothing.",
			CostUSD:    0.4213,
			HasCost:    true,
			HasResult:  true,
			NumTurns:   6,
			Duration:   93 * time.Second,
			StartedAt:  testRunDay,
			FinishedAt: testRunDay.Add(93 * time.Second),
		},
		Branch:    "lf/123-add-dry-run-to-task-clean",
		CommitSHA: "9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1",
		SHAAfter:  "9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1",
		Pushed:    true,
		Files: []git.FileChange{
			{Kind: git.ChangeModified, Path: "internal/cli/task.go"},
			{Kind: git.ChangeUntracked, Path: "internal/cli/task_clean_test.go"},
			{Kind: git.ChangeRenamed, From: "internal/cli/clean.go", Path: "internal/cli/task_clean.go"},
		},
		Output:    numberedLines("worker: applying edit to internal/cli/task.go", 60),
		BriefPath: testBriefPath,
	}
}

// failureRun is the fixture a non-zero exit produces.
func failureRun() report.Run {
	return report.Run{
		Ref:     testRef,
		Ordinal: 3,
		Result: &harness.RunResult{
			Outcome:   harness.OutcomeFailed,
			Reason:    "claude exited 1",
			ExitCode:  1,
			SessionID: "8f1a0c2e-0000-4000-8000-000000000002",
			LogPath:   testLogPath,
			Model:     "claude-sonnet-4-5",
			HasResult: true,
			IsError:   true,
			Duration:  12*time.Second + 400*time.Millisecond,
			Stderr:    "go: downloading example.com/nope v1.2.3\nerror: module example.com/nope: not found\n",
			// Dated like any other run, so that the absence of a brief link in
			// this fixture's goldens is the push gate doing its job rather than
			// a missing date quietly suppressing it.
			StartedAt:  testRunDay,
			FinishedAt: testRunDay.Add(12*time.Second + 400*time.Millisecond),
		},
		Branch: "lf/123-add-dry-run-to-task-clean",
		Files:  []git.FileChange{{Kind: git.ChangeModified, Path: "go.mod"}},
	}
}

// numberedLines builds n distinguishable lines, so a test can tell which part
// of a body survived a cut.
func numberedLines(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s [line %04d]\n", prefix, i)
	}
	return b.String()
}

// render renders a run report and fails on error.
func render(t *testing.T, rep *report.Reporter, run report.Run) redact.Body {
	t.Helper()
	body, err := rep.RenderRun(t.Context(), run)
	if err != nil {
		t.Fatalf("RenderRun: %v", err)
	}
	if !body.Valid() {
		t.Fatal("RenderRun returned a Body that reports Valid() == false")
	}
	return body
}

// checkGolden compares got against testdata/<name>.golden, or rewrites it
// under -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run `go test ./internal/report -update` to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("rendered body does not match %s.\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// containsAll fails naming each missing substring, so one run of the test
// reports every element that went missing rather than the first.
func containsAll(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not contain %q", what, want)
		}
	}
}

// TestMarkersCarryTheirAttributes covers both forms of the S-8 marker
// (AC-4.2). The run report carries run=N; the run-less reply has no run
// attribute at all, because writing run=0 would make it parseable as a report
// of a run that never existed.
func TestMarkersCarryTheirAttributes(t *testing.T) {
	t.Parallel()

	status := report.StatusMarker(testRef, 7)
	if want := "<!-- lf:status task=github:acme/acme-api#123 run=7 -->"; status != want {
		t.Errorf("StatusMarker = %q, want %q", status, want)
	}

	reply := report.ReplyMarker(testRef)
	if want := "<!-- lf:reply task=github:acme/acme-api#123 -->"; reply != want {
		t.Errorf("ReplyMarker = %q, want %q", reply, want)
	}
	if strings.Contains(reply, "run=") {
		t.Errorf("ReplyMarker %q carries a run attribute", reply)
	}

	// The self-filter matches this prefix at the start of a line, and it is what
	// keeps the daemon from reading its own comment as an instruction. Both
	// forms have to begin with it or the loop is only prevented by the identity
	// check.
	for _, marker := range []string{status, reply} {
		if !strings.Contains(marker, report.MarkerPrefix) {
			t.Errorf("marker %q does not contain the self-filter prefix %q", marker, report.MarkerPrefix)
		}
	}
}

// TestEveryBodyBeginsWithItsMarker is AC-4.2's other half: the marker is at the
// start, not merely present. A marker further down still satisfies the
// self-filter but stops being a reliable anchor for finding the comment again.
func TestEveryBodyBeginsWithItsMarker(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	runBody := render(t, rep, successRun())
	if !strings.HasPrefix(runBody.String(), report.StatusMarker(testRef, 2)) {
		t.Errorf("run report does not begin with its status marker:\n%.120s", runBody.String())
	}

	replyBody, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyUnknownCommand, Command: "/frobnicate",
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	if !strings.HasPrefix(replyBody.String(), report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with its reply marker:\n%.120s", replyBody.String())
	}
}

// TestEveryBodyTellsTheReaderHowToReply is AC-S0.6, the half of the fix that
// ships in prose rather than in the matcher.
//
// Line-anchoring the self-filter makes GitHub's quote-reply safe, but it cannot
// make a marker pasted back unquoted safe: at the start of a line that is
// litefactory's own output by definition. Every comment litefactory posts is a
// comment somebody may answer, so every one of them says how.
//
// The notice is checked on both shapes and at the smallest limit a body is ever
// assembled at, because a report whose notice was the part that did not fit
// would fail exactly the reader it is for.
func TestEveryBodyTellsTheReaderHowToReply(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	bodies := map[string]string{
		"run report":      render(t, rep, successRun()).String(),
		"failure report":  render(t, rep, failureRun()).String(),
		"run-less reply":  renderReply(t, rep, report.Reply{Ref: testRef, Kind: report.ReplyEmptySpike, Command: "/spike"}),
		"tools refusal":   renderReply(t, rep, report.Reply{Ref: testRef, Kind: report.ReplyLegacyToolLabel}),
		"unknown command": renderReply(t, rep, report.Reply{Ref: testRef, Kind: report.ReplyUnknownCommand, Command: "/frobnicate"}),
	}

	for name, body := range bodies {
		if !strings.Contains(body, report.ReplyPlainlyNotice) {
			t.Errorf("the %s does not tell the reader how to reply:\n%s", name, body)
		}
		// The notice is about the marker and must not contain one: a second
		// marker in a body would sit somewhere other than byte zero, which is
		// the property the self-filter now anchors on.
		if got := strings.Count(body, report.MarkerPrefix); got != 1 {
			t.Errorf("the %s carries %d markers, want exactly the one it opens with", name, got)
		}
	}

	if strings.Contains(report.ReplyPlainlyNotice, report.MarkerPrefix) {
		t.Error("ReplyPlainlyNotice contains the marker prefix it exists to warn about")
	}

	// A report squeezed into a quarter of GitHub's limit still carries it: the
	// notice is in the never-cut section, above the log tail (AC-4.8).
	small, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Limit:    4_000,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	run := successRun()
	run.Output = numberedLines("worker: applying edit to internal/cli/task.go", 60)
	if body := render(t, small, run).String(); !strings.Contains(body, report.ReplyPlainlyNotice) {
		t.Errorf("a truncated report dropped the notice:\n%s", body)
	}
}

// renderReply renders a run-less reply and fails on error.
func renderReply(t *testing.T, rep *report.Reporter, reply report.Reply) string {
	t.Helper()
	body, err := rep.RenderReply(t.Context(), reply)
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	return body.String()
}

// TestSuccessReport walks AC-4.3 element by element and then pins the whole
// body to a golden file.
func TestSuccessReport(t *testing.T) {
	t.Parallel()

	run := successRun()
	body := render(t, newReporter(t), run).String()

	containsAll(t, "success report", body,
		"succeeded",
		run.Result.ResultText,             // final result text
		"internal/cli/task_clean_test.go", // files touched
		"renamed internal/cli/clean.go -> internal/cli/task_clean.go",
		"Files touched (3)",
		run.Branch,    // branch name
		run.CommitSHA, // commit SHA, plain text
		"(pushed)",
		"**Exit status:** `0`", // exit status
		"**Duration:** 1m33s",  // duration
		testLogPath,            // where the unredacted copy is
		"<details>",            // collapsible output
		"</details>",
		fmt.Sprintf("last %d lines", report.TailLines),
	)

	// The commit itself is still plain text — the SHA is a fact to quote, not a
	// destination. What is linked is the brief, and only ever pinned to a
	// commit: a bare "/blob/" check would pass a branch-relative URL, which
	// shows whatever the branch holds when it is opened rather than what this
	// run produced.
	if strings.Contains(body, "https://github.com/acme/acme-api/commit") {
		t.Error("success report links the commit page; the SHA is rendered as plain text")
	}
	if want := "/blob/" + run.SHAAfter + "/"; !strings.Contains(body, want) {
		t.Errorf("success report has no permalink pinned to sha_after; want %q in:\n%s", want, body)
	}
	if strings.Contains(body, "/blob/"+run.Branch+"/") {
		t.Error("success report carries a branch-relative blob URL, which is not pinned to this run")
	}

	// The <details> block carries the *last* TailLines lines, not the first.
	if !strings.Contains(body, "[line 0060]") {
		t.Error("output tail does not reach the last line of the run's output")
	}
	if strings.Contains(body, "[line 0020]") {
		t.Errorf("output tail reaches back to line 20, so it is longer than %d lines", report.TailLines)
	}

	checkGolden(t, "run_succeeded", body)
}

// TestFailureReport covers AC-4.5 and S-9: distinguishable from a success at a
// glance, carrying the exit code, the elapsed duration, the absolute local log
// path and the quoted tail.
func TestFailureReport(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	failure := render(t, rep, failureRun()).String()
	success := render(t, rep, successRun()).String()

	containsAll(t, "failure report", failure,
		"failed",
		"> [!CAUTION]",         // the callout that makes it obvious
		"exited with status 1", // the exit code, in words
		"**Exit status:** `1`",
		"**Reason:** claude exited 1", // the reason the harness recorded (S-9)
		"**Duration:** 12.4s",         // elapsed duration
		testLogPath,                   // absolute local log path
		"<details>",
		"module example.com/nope: not found", // the stderr tail, quoted
	)

	if !filepath.IsAbs(testLogPath) {
		t.Fatalf("the fixture log path %q is not absolute", testLogPath)
	}

	// "At a glance" means the difference is in the first lines, not buried in
	// the metadata: the heading word and the callout both have to differ.
	if headline(failure) == headline(success) {
		t.Errorf("failure and success share the heading %q", headline(failure))
	}
	if strings.Contains(success, "[!CAUTION]") {
		t.Error("the success report carries a caution callout")
	}

	checkGolden(t, "run_failed", failure)
}

// headline returns the body's heading line.
func headline(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "### ") {
			return line
		}
	}
	return ""
}

// TestEveryOutcomeRendersDistinguishably is the reason harness.Outcome has five
// values rather than a bool. The two that are easiest to get wrong are the ones
// asserted hardest: an abandoned run means the daemon was stopping and is
// nobody's fault, and a no-result run needs to tell its reader what to actually
// do about it.
func TestEveryOutcomeRendersDistinguishably(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	// The reasons are the ones internal/harness actually records, so what the
	// goldens show is what an issue would show.
	reasons := map[harness.Outcome]string{
		harness.OutcomeSucceeded: "",
		harness.OutcomeFailed:    "claude exited with status 1",
		harness.OutcomeTimedOut:  "timed out after 30m0s; the process subtree was killed",
		harness.OutcomeAbandoned: "abandoned: the caller's run context ended before claude finished (daemon shutdown or cancellation)",
		harness.OutcomeNoResult:  "claude exited 0 without emitting a terminal result event",
	}
	outcomes := []harness.Outcome{
		harness.OutcomeSucceeded,
		harness.OutcomeFailed,
		harness.OutcomeTimedOut,
		harness.OutcomeAbandoned,
		harness.OutcomeNoResult,
	}

	headings := make(map[string]harness.Outcome, len(outcomes))
	bodies := make(map[harness.Outcome]string, len(outcomes))
	for _, outcome := range outcomes {
		run := failureRun()
		run.Result.Outcome = outcome
		run.Result.Reason = reasons[outcome]
		if outcome != harness.OutcomeFailed {
			run.Result.ExitCode = harness.SignalExitCode
		}
		if outcome == harness.OutcomeSucceeded {
			run.Result.ExitCode = 0
		}

		body := render(t, rep, run).String()
		bodies[outcome] = body

		head := headline(body)
		if prev, dup := headings[head]; dup {
			t.Errorf("%s and %s render the same heading %q", prev, outcome, head)
		}
		headings[head] = outcome

		// S-9 wants the exit code *or the kill reason* in every failure
		// comment. The three killed outcomes have no exit status to give, so
		// the reason is the only thing that says what happened to them.
		if reason := reasons[outcome]; reason != "" && !strings.Contains(body, "**Reason:** "+reason) {
			t.Errorf("the %s report does not carry the recorded reason %q", outcome, reason)
		}

		checkGolden(t, "outcome_"+outcome.String(), body)
	}

	// An abandonment is not a crash and not a timeout. Reading it as either
	// sends someone looking for a fault that is not there, so neither the
	// heading nor the callout may present it as one.
	abandoned := bodies[harness.OutcomeAbandoned]
	containsAll(t, "the abandoned report", abandoned, "abandoned", "shutting down", "Comment again")
	for _, wrong := range []string{"failed", "timed out", "crashed"} {
		if strings.Contains(headline(abandoned), wrong) {
			t.Errorf("the abandoned report's heading %q reads as %q, which it is not", headline(abandoned), wrong)
		}
	}
	if strings.Contains(abandoned, "[!CAUTION]") {
		t.Error("the abandoned report raises a caution callout for something that is nobody's fault")
	}

	// A no-result run is the SD-3 stall. The report has to name the likely
	// cause, because "no result" on its own is not actionable.
	containsAll(t, "the no-result report", bodies[harness.OutcomeNoResult],
		"without ever reporting a result", "credential", "logged in")

	// A timeout says what to change; a plain failure quotes the exit status.
	containsAll(t, "the timed-out report", bodies[harness.OutcomeTimedOut], "timeout", "still in the worktree")
	containsAll(t, "the failed report", bodies[harness.OutcomeFailed], "exited with status 1")
}

// TestAFailureAroundTheRunIsItsOwnReport covers the two shapes that are not
// about how `claude` ended: a turn that never reached it, and a run that did
// its work but could not be pushed.
//
// The second is the one worth being careful about. Reporting it as a success
// tells someone to go and look for a branch that is not on the remote, so the
// report says what happened and names the worktree the commit is still in
// (AC-3.24).
func TestAFailureAroundTheRunIsItsOwnReport(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	t.Run("never reached claude", func(t *testing.T) {
		t.Parallel()

		body := render(t, rep, report.Run{
			Ref:     testRef,
			Ordinal: 1,
			Failure: errors.New("git worktree add: fatal: 'lf/123-x' is already checked out at /Users/mario/dev/.lf-worktrees/acme-api/task-123"),
			Branch:  "lf/123-add-dry-run-to-task-clean",
		}).String()

		containsAll(t, "the prepare-failure report", body,
			"did not run", "> [!CAUTION]", "already checked out")
		if strings.Contains(headline(body), "succeeded") {
			t.Errorf("a turn that never ran reports as a success: %q", headline(body))
		}
		checkGolden(t, "run_never_started", body)
	})

	t.Run("push failed after a good run", func(t *testing.T) {
		t.Parallel()

		run := successRun()
		run.Pushed = false
		run.Failure = errors.New("git push origin lf/123-add-dry-run-to-task-clean: permission denied (publickey)")
		run.WorktreePath = "/Users/mario/dev/.lf-worktrees/acme-api/task-123"

		body := render(t, rep, run).String()

		containsAll(t, "the push-failure report", body,
			"not completed",
			"> [!CAUTION]",
			"permission denied",
			run.WorktreePath,
			"not pushed",
			run.CommitSHA,         // the commit is real and is named
			run.Result.ResultText, // and so is what the run did
		)
		if strings.Contains(headline(body), "succeeded") {
			t.Errorf("an unpushed run reports as a success: %q", headline(body))
		}
		checkGolden(t, "run_push_failed", body)
	})
}

// TestOversizeBodyCutsTheLogTailAndNeverTheSummary is AC-4.8. The log tail is
// the expendable part — the whole of it is on disk — and the summary and the
// path to that disk copy are what a human needs, so they are what survives.
func TestOversizeBodyCutsTheLogTailAndNeverTheSummary(t *testing.T) {
	t.Parallel()

	run := successRun()
	// Forty lines, each far larger than a whole comment: the tail alone is
	// twelve times the limit.
	run.Output = strings.Repeat(strings.Repeat("x", 20_000)+"\n", 40)
	run.Output = strings.ReplaceAll(run.Output, "xxxxxxxxxx\n", "END-OF-LINE\n")

	body := render(t, newReporter(t), run)
	text := body.String()

	if body.Len() > github.CommentSizeLimit {
		t.Errorf("body is %d bytes, over GitHub's %d-byte limit", body.Len(), github.CommentSizeLimit)
	}

	// The summary survives whole: every element AC-4.3 lists is still there.
	containsAll(t, "the truncated body", text,
		report.StatusMarker(testRef, 2),
		run.Result.ResultText,
		run.Branch,
		run.CommitSHA,
		"Files touched (3)",
		"internal/cli/task_clean_test.go",
		"**Exit status:** `0`",
		"**Duration:** 1m33s",
	)

	// The truncation is explicit, and it names the local log — the two things
	// that stop a shortened comment from being read as a complete one.
	containsAll(t, "the truncated body", text, "removed here to fit the comment size limit", testLogPath)

	// The cut came out of the middle: both ends of the tail survive.
	if !strings.Contains(text, "END-OF-LINE") {
		t.Error("no run output survived the cut at all")
	}
	if !strings.HasSuffix(strings.TrimRight(text, "\n"), "</details>") {
		t.Error("the body does not end with the closing </details>, so the cut ran past the tail")
	}
}

// truncatedReportLimit is the limit TestTruncatedReportReadsAsOne renders at:
// small enough that the report has to be cut and can be read in one screen,
// large enough that the log tail is still worth quoting rather than dropped
// whole.
//
// It was 4000 until the brief link landed in the metadata list. The link is a
// forge URL — around 300 bytes with its path, against a budget of limit less
// [redact.Margin] — and at 4000 it took the log tail below [minTailBytes], so
// this test stopped exercising the middle-elision it was written for and
// started exercising the drop-it-whole branch instead. The number moved rather
// than the assertions, because what is under test here is the elision.
// Dropping the tail whole at a small limit is covered by the goldens.
const truncatedReportLimit = 5_000

// TestTruncatedReportReadsAsOne renders the AC-4.8 result at a limit small
// enough to read in one screen. The golden is the actual review: someone has to
// be able to look at a shortened report and tell that it is shortened, and know
// where the rest of it went.
func TestTruncatedReportReadsAsOne(t *testing.T) {
	t.Parallel()

	rep, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Limit:    truncatedReportLimit,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}

	run := successRun()
	run.Output = numberedLines("worker: applying edit to internal/cli/task.go", 60)

	body := render(t, rep, run)
	text := body.String()

	if body.Len() > truncatedReportLimit {
		t.Errorf("body is %d bytes, over the %d-byte limit", body.Len(), truncatedReportLimit)
	}
	// Both ends of the tail survive and the middle is gone: the first lines say
	// what the run was doing and the last say how it stopped.
	containsAll(t, "the truncated report", text,
		"[line 0021]", "[line 0060]", "of output removed here", testLogPath)
	if strings.Contains(text, "[line 0040]") {
		t.Error("the cut did not come out of the middle of the tail")
	}

	checkGolden(t, "run_truncated", text)
}

// TestOversizeBodyIsWithinTheLimitAfterRedaction checks the interaction the
// spec left unspecified: redaction runs after the body is assembled and can
// grow it, so the size that matters is the one measured after the pass, not
// before. The fixture is dense with eight-character secrets, each of which
// becomes the ten-byte "[redacted]".
func TestOversizeBodyIsWithinTheLimitAfterRedaction(t *testing.T) {
	t.Parallel()

	rep := newReporter(t, func(o *redact.Options) {
		o.Environ = []string{"DEPLOY_SECRET=abcdefgh"}
	})

	run := successRun()
	run.Output = strings.Repeat("abcdefgh abcdefgh abcdefgh abcdefgh\n", 4_000)
	run.Result = run.Result.Clone()
	run.Result.ResultText = strings.Repeat("abcdefgh ", 8_000)

	body := render(t, rep, run)
	if body.Len() > github.CommentSizeLimit {
		t.Errorf("body is %d bytes after redaction, over the %d-byte limit", body.Len(), github.CommentSizeLimit)
	}
	if strings.Contains(body.String(), "abcdefgh") {
		t.Error("a known secret survived into the outbound body")
	}
	if body.Counts().Total() == 0 {
		t.Error("no redactions were counted, so the fixture did not exercise the pass")
	}
}

// TestTokenShapedOutputNeverReachesTheBody is AC-4.6 from the outbound end. The
// same string has to remain verbatim in $LF_HOME/logs, which is asserted from
// the other end in internal/redact and LF-5; what this test owns is that no
// path through this package — result text, stderr tail, reason, branch name —
// carries one out.
func TestTokenShapedOutputNeverReachesTheBody(t *testing.T) {
	t.Parallel()

	const botToken = "ghp_liveBotTokenValue0000000000000000"
	const envSecret = "s3cret-deploy-key-value"

	rep := newReporter(t, func(o *redact.Options) {
		o.Token = secrets.NewToken(botToken)
		o.Environ = []string{"DEPLOY_TOKEN=" + envSecret}
	})

	run := successRun()
	run.Result = run.Result.Clone()
	run.Result.ResultText = "I exported the token as " + envSecret + " and it worked."
	run.Result.Stderr = "curl: Authorization: Bearer " + botToken + "\n"
	run.Result.Reason = "auth failed for " + botToken
	run.Output = "fatal: could not read from https://mario:" + envSecret + "@github.com/acme/acme-api.git\n" +
		"ghp_anotherTokenShapedStringABCDEFGHIJ0123\nAKIAIOSFODNN7EXAMPLE\n"

	// The local copy is untouched: this is the exact text that stays in the
	// JSONL on disk, and it still holds every secret verbatim.
	local := run.Result.ResultText + run.Result.Stderr + run.Output
	containsAll(t, "the local, unredacted copy", local, botToken, envSecret, "AKIAIOSFODNN7EXAMPLE")

	body := render(t, rep, run).String()
	for _, secret := range []string{botToken, envSecret, "AKIAIOSFODNN7EXAMPLE", "ghp_anotherTokenShapedStringABCDEFGHIJ0123"} {
		if strings.Contains(body, secret) {
			t.Errorf("the outbound body contains the credential %q", secret)
		}
	}
	if !strings.Contains(body, "[redacted]") {
		t.Error("nothing in the body was replaced, so the fixture never reached the pass")
	}
}

// TestSparseRunResultStillReports is AC-4.1 at its hardest: a run whose result
// is missing or half-filled still produces a comment a human can act on.
// Silence is the one outcome that is not allowed, and the run that crashed on
// its way to reporting is the run whose comment matters most.
func TestSparseRunResultStillReports(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	t.Run("no result at all", func(t *testing.T) {
		t.Parallel()

		body := render(t, rep, report.Run{Ref: testRef, Ordinal: 4}).String()

		containsAll(t, "the crash report", body,
			report.StatusMarker(testRef, 4),
			"outcome unknown",
			"> [!CAUTION]",
			"**Exit status:** unknown",
			"**Duration:** unknown",
		)
		// It must not claim a success it has no evidence for.
		if strings.Contains(headline(body), "succeeded") {
			t.Errorf("a run with no result reports as a success: %q", headline(body))
		}
		checkGolden(t, "run_no_result_recorded", body)
	})

	t.Run("half-filled result", func(t *testing.T) {
		t.Parallel()

		body := render(t, rep, report.Run{
			Ref:     testRef,
			Ordinal: 5,
			Result: &harness.RunResult{
				Outcome:  harness.OutcomeFailed,
				ExitCode: harness.SignalExitCode,
				LogPath:  testLogPath,
			},
		}).String()

		containsAll(t, "the sparse failure report", body,
			"failed",
			"killed by litefactory",
			"**Duration:** unknown",
			testLogPath,
			"No files were changed.",
			"No output was captured",
		)
		checkGolden(t, "run_sparse_result", body)
	})
}

// TestUntrustedTextCannotBreakOutOfItsBlock covers the markup half of
// "untrusted content": the run output is written by a model that read a
// third-party issue comment, and quoting it with a fixed three-backtick fence
// would let it close the fence and continue as markup.
func TestUntrustedTextCannotBreakOutOfItsBlock(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Output = "harmless\n```\n</details>\n### not a heading\n``` \nmore\n"
	run.Files = []git.FileChange{{Kind: git.ChangeUntracked, Path: "weird/```name.txt"}}

	body := render(t, newReporter(t), run).String()

	// The fence around the output is longer than any run of backticks inside
	// it, so the block closes where this package says it does.
	open := strings.Index(body, "````text\n")
	closing := strings.LastIndex(body, "````")
	if open < 0 || closing <= open {
		t.Fatalf("the output block does not use a widened fence:\n%s", body)
	}
	if headline(body) == "### not a heading" {
		t.Error("output escaped its code block and became a heading")
	}

	// The </details> the output carries sits inside the fence; the one that
	// closes the block is the last thing in the body.
	if quoted := strings.Index(body, "</details>"); quoted < open || quoted > closing {
		t.Errorf("the quoted </details> at %d is not inside the fence (%d..%d)", quoted, open, closing)
	}
	if !strings.HasSuffix(strings.TrimRight(body, "\n"), "</details>") {
		t.Error("the block is not closed at the end of the body")
	}
}

// TestControlCharactersAreStripped guards against the 422 that reads like a
// mystery: GitHub rejects a body that is not valid UTF-8, and half-stripped
// ANSI escapes render as junk.
func TestControlCharactersAreStripped(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Output = "\x1b[31mred\x1b[0m text\x00 and \xff invalid\r\n"

	body := render(t, newReporter(t), run).String()

	for _, bad := range []string{"\x1b", "\x00", "\r", "[31m"} {
		if strings.Contains(body, bad) {
			t.Errorf("body still contains %q", bad)
		}
	}
	containsAll(t, "the body", body, "red text and")
}

// TestReplies covers run-less reply forms. The wording matters as much as the
// marker because it tells an owner how Litefactory interpreted their comment.
func TestReplies(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	cases := []struct {
		name   string
		reply  report.Reply
		golden string
		wants  []string
	}{
		{
			name:   "unknown command",
			reply:  report.Reply{Ref: testRef, Kind: report.ReplyUnknownCommand, Command: "/frobnicate"},
			golden: "reply_unknown",
			wants:  []string{"do not recognise `/frobnicate`", "`/schedule`"},
		},
		{
			name:   "empty spike",
			reply:  report.Reply{Ref: testRef, Kind: report.ReplyEmptySpike, Command: "/spike"},
			golden: "reply_empty_spike",
			wants:  []string{"`/spike` needs something to work on", "/spike see whether"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := rep.RenderReply(t.Context(), tc.reply)
			if err != nil {
				t.Fatalf("RenderReply: %v", err)
			}
			text := body.String()

			if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
				t.Errorf("reply does not begin with the reply marker:\n%s", text)
			}
			if strings.Contains(text, "run=") {
				t.Error("a run-less reply carries a run attribute")
			}
			containsAll(t, tc.name+" reply", text, tc.wants...)
			checkGolden(t, tc.golden, text)
		})
	}
}

// TestReplyQuotesTheCommandSafely re-applies the parser's own restriction at
// the point the token is echoed back. This is the one place a third party's
// text is repeated in a comment litefactory signs its own name to.
func TestReplyQuotesTheCommandSafely(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref:     testRef,
		Kind:    report.ReplyUnknownCommand,
		Command: "/ev`il <!-- lf:status --> @everyone",
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if strings.Contains(text, "@everyone") {
		t.Error("the reply echoed a mention back into the comment")
	}
	if strings.Count(text, report.MarkerPrefix) != 1 {
		t.Error("the reply echoed a second lf: marker back into the comment")
	}
	// The token stops at the backtick rather than being reassembled around it.
	if !strings.Contains(text, "`/ev`") {
		t.Errorf("the reply does not quote the sanitised command:\n%s", text)
	}
	if strings.Contains(text, "il") {
		t.Errorf("the reply reassembled text from beyond the first bad character:\n%s", text)
	}
}

// TestRenderRejectsUnrenderableInput covers the only inputs that fail: the ones
// that would produce a body without a well-formed marker. Everything else
// degrades to "unknown" rather than erroring, because a run that finished has
// to be reported.
func TestRenderRejectsUnrenderableInput(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	cases := []struct {
		name string
		run  report.Run
	}{
		{"empty ref", report.Run{Ordinal: 1}},
		{"ref that could close the comment", report.Run{
			Ref:     naming.Ref{Forge: "github", Owner: "acme --> x", Repo: "api", Issue: 1},
			Ordinal: 1,
		}},
		{"zero ordinal", report.Run{Ref: testRef, Ordinal: 0}},
		{"negative ordinal", report.Run{Ref: testRef, Ordinal: -1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := rep.RenderRun(t.Context(), tc.run)
			if !errors.Is(err, report.ErrInvalidArgument) {
				t.Fatalf("RenderRun error = %v, want ErrInvalidArgument", err)
			}
			if body.Valid() {
				t.Error("a failed render returned a postable Body")
			}
		})
	}

	if _, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef}); !errors.Is(err, report.ErrInvalidArgument) {
		t.Errorf("RenderReply with the zero ReplyKind: error = %v, want ErrInvalidArgument", err)
	}
}

// TestNewRejectsIncompleteWiring fails at startup rather than at the first
// render, which happens just after a run finished — the worst moment to find
// out that nothing can report it.
func TestNewRejectsIncompleteWiring(t *testing.T) {
	t.Parallel()

	if _, err := report.New(report.Options{Limit: github.CommentSizeLimit}); !errors.Is(err, report.ErrInvalidArgument) {
		t.Errorf("New without a redactor: error = %v, want ErrInvalidArgument", err)
	}
	if _, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}})}); !errors.Is(err, report.ErrInvalidArgument) {
		t.Errorf("New with a zero limit: error = %v, want ErrInvalidArgument", err)
	}
}

// TestRenderFailsClosedOnRedactionFailure is AC-4.9 seen from this package:
// when the pass cannot complete, no body comes back at all, so there is nothing
// for the caller to post and the run stays un-reported.
//
// The failure is induced with a Redactor that never went through redact.New,
// which is the one Apply failure reachable from outside that package. What is
// asserted is the shape of the refusal, not the cause: no Body, and an error
// that identifies itself as a redaction failure.
func TestRenderFailsClosedOnRedactionFailure(t *testing.T) {
	t.Parallel()

	rep, err := report.New(report.Options{
		Redactor: &redact.Redactor{},
		Limit:    github.CommentSizeLimit,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	if rep.Limit() != github.CommentSizeLimit {
		t.Errorf("Limit() = %d, want %d", rep.Limit(), github.CommentSizeLimit)
	}

	body, err := rep.RenderRun(t.Context(), successRun())
	if err == nil {
		t.Fatal("RenderRun succeeded with a redactor that cannot redact")
	}
	if body.Valid() {
		t.Error("a failed render returned a postable Body")
	}
	if !errors.Is(err, redact.ErrRedactionFailed) {
		t.Errorf("error %v does not wrap redact.ErrRedactionFailed", err)
	}

	replyBody, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyEmptySpike})
	if err == nil || replyBody.Valid() {
		t.Errorf("RenderReply returned (%v, %v); want no Body and an error", replyBody.Valid(), err)
	}
}

// TestASmallLimitStillProducesAPostableBody covers the other end of the size
// question. A limit far below what a report wants is not an error: the body is
// trimmed to fit and still says where the whole of it is.
func TestASmallLimitStillProducesAPostableBody(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{600, 1_500, 4_000, 16_000, github.CommentSizeLimit} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			t.Parallel()

			rep, err := report.New(report.Options{
				Redactor: redact.New(redact.Options{Environ: []string{}}),
				Limit:    limit,
			})
			if err != nil {
				t.Fatalf("report.New: %v", err)
			}

			run := successRun()
			run.Output = numberedLines("worker: a line of run output that is not especially short", 40)

			body := render(t, rep, run)
			if body.Len() > limit {
				t.Errorf("body is %d bytes, over the %d-byte limit", body.Len(), limit)
			}
			// The marker survives every level of trimming, or the self-filter
			// stops recognising litefactory's own comments.
			if !strings.HasPrefix(body.String(), report.StatusMarker(testRef, 2)) {
				t.Errorf("the marker did not survive:\n%s", body.String())
			}
		})
	}
}

// TestToolsBulletRendersRequestedAndResolvedSurface is AC-4.2 and AC-4.3: the
// four cases the implementer measured, pinned by name. The last case is a
// deliberate judgement call — metadataList's documented rule is to omit a fact
// rather than guess at it, and this is the test that locks that rule in for
// the Tools bullet specifically.
func TestToolsBulletRendersRequestedAndResolvedSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modify     func(*report.Run)
		want       string
		wantAbsent bool
	}{
		{
			name: "granted profile/lists the resolved surface and its profile",
			modify: func(r *report.Run) {
				r.Result.Tools = []string{"Read", "Grep", "Bash"}
				r.ToolProfile = "research"
			},
			want: "- **Tools:** `Read`, `Grep`, `Bash` — profile `research`",
		},
		{
			name: "requested exceeds resolved/names the tool that was not granted (TodoWrite worked example)",
			modify: func(r *report.Run) {
				r.Result.Tools = todoWriteResolved
				r.ToolsRequested = todoWriteRequested
			},
			want: "- **Tools:** `Task`, `Bash`, `Edit`, `Glob`, `Grep`, `Read`, `Write` — " +
				"no profile (the operator's default surface) — requested but not granted: `TodoWrite`",
		},
		{
			name: "nil RunResult/shows the requested surface and the profile, never a resolved one",
			modify: func(r *report.Run) {
				r.Result = nil
				r.ToolsRequested = []string{"Read", "Grep"}
				r.ToolProfile = "research"
			},
			want: "- **Tools:** requested `Read`, `Grep`, but the run recorded no resolved surface — profile `research`",
		},
		{
			name: "no profile at all/says so explicitly rather than staying silent",
			modify: func(r *report.Run) {
				r.Result.Tools = []string{"Read", "Edit"}
			},
			want: "- **Tools:** `Read`, `Edit` — no profile (the operator's default surface)",
		},
		{
			name: "nothing known about tools/the bullet is omitted rather than guessed",
			modify: func(r *report.Run) {
				r.Result = nil
			},
			wantAbsent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := successRun()
			// Start from nothing tools-related recorded, so every case's
			// expectation is self-contained rather than riding successRun's
			// own defaults.
			run.Result.Tools = nil
			run.ToolsRequested = nil
			run.ToolProfile = ""
			tt.modify(&run)

			text := render(t, newReporter(t), run).String()

			if tt.wantAbsent {
				if strings.Contains(text, "**Tools:**") {
					t.Errorf("expected no Tools bullet, got:\n%s", text)
				}
				return
			}
			if !strings.Contains(text, tt.want) {
				t.Errorf("rendered report does not contain %q:\n%s", tt.want, text)
			}
		})
	}
}

// TestToolsBulletFollowsModelImmediately is the positional half of AC-4.2: the
// bullet must land in metadataList right after Model, which is what puts it
// inside secFixed — the one section [runBody] never trims.
func TestToolsBulletFollowsModelImmediately(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Result.Tools = []string{"Read", "Grep", "Bash"}
	run.ToolProfile = "research"

	text := render(t, newReporter(t), run).String()
	lines := strings.Split(text, "\n")

	modelIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "- **Model:**") {
			modelIdx = i
			break
		}
	}
	if modelIdx == -1 {
		t.Fatalf("no Model bullet found:\n%s", text)
	}
	if modelIdx+1 >= len(lines) {
		t.Fatalf("Model is the last line of the body; Tools cannot follow it:\n%s", text)
	}
	if next := lines[modelIdx+1]; !strings.HasPrefix(next, "- **Tools:**") {
		t.Errorf("Tools does not immediately follow Model; the next line is %q", next)
	}
}

// TestToolsBulletSurvivesTruncation is AC-4.2's second, load-bearing
// assertion: the bullet must still be there under a report big enough to
// force every other section to be cut, because it is the only after-the-fact
// evidence of which tool surface a per-tick-resolved run actually used (C3).
// Asserting only the bullet's position, without this, would miss the whole
// point of putting it in secFixed.
func TestToolsBulletSurvivesTruncation(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Result.Tools = []string{"Read", "Grep", "Bash"}
	run.ToolProfile = "research"
	run.ToolsRequested = []string{"Read", "Grep", "Bash", "TodoWrite"}
	// Forty lines, each far larger than a whole comment: enough to force the
	// log tail to be elided or dropped outright.
	run.Output = strings.Repeat(strings.Repeat("x", 20_000)+"\n", 40)

	body := render(t, newReporter(t), run)
	text := body.String()

	if body.Len() > github.CommentSizeLimit {
		t.Errorf("body is %d bytes, over GitHub's %d-byte limit", body.Len(), github.CommentSizeLimit)
	}
	want := "- **Tools:** `Read`, `Grep`, `Bash` — profile `research` — requested but not granted: `TodoWrite`"
	if !strings.Contains(text, want) {
		t.Errorf("the Tools bullet did not survive truncation; want %q in:\n%s", want, text)
	}
}

// TestToolsBulletPassesThroughRedactionUntouched is AC-4.6: internal/redact
// matches credential shapes only, so a realistic tool list and profile name
// must come out exactly as they went in, with no redaction class firing on
// them at all.
func TestToolsBulletPassesThroughRedactionUntouched(t *testing.T) {
	t.Parallel()

	redactor := redact.New(redact.Options{Environ: []string{"DEPLOY_SECRET=abcdefgh", "API_KEY=shhhhhhh1"}})
	line := "- **Tools:** `Task`, `Bash`, `Edit`, `Glob`, `Grep`, `Read`, `Write` — " +
		"profile `research` — requested but not granted: `TodoWrite`"

	body, err := redactor.Apply(t.Context(), line, 10_000)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if body.String() != line {
		t.Errorf("a tool list was altered by redaction:\ngot:  %s\nwant: %s", body.String(), line)
	}
	if total := body.Counts().Total(); total != 0 {
		t.Errorf("redaction fired %d times on a tool list and profile name: %+v", total, body.Counts())
	}
}

// TestToolsBulletCarriesNoLabelSyntax is the C2/S-13 guardrail: nothing
// originating from GitHub — a label body, the "lf:tools=" syntax itself — may
// ever reach a rendered report. ToolProfile is documented as a validated
// config key, never raw label text.
func TestToolsBulletCarriesNoLabelSyntax(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Result.Tools = todoWriteResolved
	run.ToolsRequested = todoWriteRequested
	run.ToolProfile = "research"

	text := render(t, newReporter(t), run).String()

	for _, forbidden := range []string{"lf:tools=", "lf:tools ="} {
		if strings.Contains(text, forbidden) {
			t.Errorf("rendered report leaked label syntax %q:\n%s", forbidden, text)
		}
	}
}

// TestRunCarriesNoLabelField is the structural half of the same guardrail:
// report.Run has no field for raw label text at all, so there is nothing for a
// future change to wire up by mistake (S-13).
func TestRunCarriesNoLabelField(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf(report.Run{})
	for i := range rt.NumField() {
		name := rt.Field(i).Name
		if strings.Contains(strings.ToLower(name), "label") {
			t.Errorf("report.Run has a field named %q; labels must never reach this package (S-13)", name)
		}
	}
}

// No rendered body escapes this package as a string. This is the guarantee the
// package exists to preserve, and it is asserted at compile time where a reader
// will look for it: both renderers hand back a redact.Body, which only
// redact.Apply can construct and which github.PostComment is the only consumer
// of. A signature returning a string would put an unredacted body in a caller's
// hand and turn AC-4.6 back into a rule everyone has to remember.
//
// Changing either signature to return a string breaks this file.
var (
	_ func(*report.Reporter, context.Context, report.Run) (redact.Body, error)   = (*report.Reporter).RenderRun
	_ func(*report.Reporter, context.Context, report.Reply) (redact.Body, error) = (*report.Reporter).RenderReply
)

// testWorktreePath is the worktree path every US-3 fixture in this file uses,
// for a run that did not succeed but left its own worktree waiting.
const testWorktreePath = "/Users/mario/dev/.lf-worktrees/acme-api/task-123"

// TestNonSuccessReportNamesTheWorktree is US-3 (T-3.3, AC-3.1): the report for
// a run that did not succeed names the worktree path holding the uncommitted
// work, does not claim a commit, and must not describe "committed nothing" in
// the words a successful, no-change run uses for the same phrase.
func TestNonSuccessReportNamesTheWorktree(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	outcomes := []harness.Outcome{harness.OutcomeFailed, harness.OutcomeTimedOut, harness.OutcomeNoResult}
	for _, outcome := range outcomes {
		t.Run(outcome.String(), func(t *testing.T) {
			t.Parallel()

			run := failureRun()
			run.Result.Outcome = outcome
			if outcome != harness.OutcomeFailed {
				run.Result.ExitCode = harness.SignalExitCode
			}
			run.WorktreePath = testWorktreePath

			body := render(t, rep, run).String()

			if got := strings.Count(body, testWorktreePath); got != 1 {
				t.Errorf("the %s report names the worktree %d times, want exactly 1:\n%s", outcome, got, body)
			}
			if strings.Contains(body, "this run committed nothing") {
				t.Errorf("the %s report uses the success-only wording for committing nothing:\n%s", outcome, body)
			}
			if strings.Contains(body, "(pushed)") {
				t.Errorf("the %s report claims a commit was pushed:\n%s", outcome, body)
			}
		})
	}

	// The succeeded report is unaffected: it still uses its own, distinct
	// wording and never names a worktree for work that was already committed.
	success := successRun()
	success.WorktreePath = testWorktreePath
	successBody := render(t, rep, success).String()
	if strings.Contains(successBody, testWorktreePath) {
		t.Errorf("the succeeded report names the worktree, which only a non-success run should:\n%s", successBody)
	}

	noChange := successRun()
	noChange.CommitSHA = ""
	noChange.SHAAfter = ""
	noChange.Pushed = false
	noChange.Files = nil
	noChange.WorktreePath = testWorktreePath
	noChangeBody := render(t, rep, noChange).String()
	if !strings.Contains(noChangeBody, "this run committed nothing") {
		t.Errorf("a succeeded, no-change report lost its own wording:\n%s", noChangeBody)
	}
	if strings.Contains(noChangeBody, testWorktreePath) {
		t.Errorf("a succeeded, no-change report names the worktree; nothing was left uncommitted:\n%s", noChangeBody)
	}
}

// TestWorktreePathIsRenderedThroughTheSanitiserSpan is US-3 (T-3.4): the
// worktree path reaches the comment through the package's existing
// codeSpan/sanitiser helpers, the same as every other machine-controlled path
// this package quotes — so a path carrying backticks or a newline cannot
// break the comment's markdown or its own quoting.
func TestWorktreePathIsRenderedThroughTheSanitiserSpan(t *testing.T) {
	t.Parallel()

	// Three consecutive backticks force codeSpan's delimiter to widen past the
	// default single backtick, which is the only way to observe from outside
	// the package that codeSpan's own collision-avoidance ran at all. The
	// embedded newline exercises the same flattening every other quoted path
	// in this package gets.
	const maliciousPath = "/tmp/wt/```injected```\nrm -rf /"

	run := failureRun()
	run.WorktreePath = maliciousPath

	body := render(t, newReporter(t), run)
	text := body.String()

	if !body.Valid() {
		t.Fatalf("a hostile worktree path produced an invalid comment body:\n%s", text)
	}
	// The raw, multi-line path must never appear verbatim: codeSpan flattens
	// it to one line before quoting it, exactly as it does for every other
	// path this package renders.
	if strings.Contains(text, maliciousPath) {
		t.Errorf("the raw, multi-line worktree path reached the comment unflattened:\n%s", text)
	}
	// codeSpan widens its delimiter past the longest run of backticks in the
	// content, so three consecutive backticks in the path force a four-tick
	// delimiter around it.
	if !strings.Contains(text, "````") {
		t.Errorf("the worktree path's triple backtick did not force codeSpan's wider delimiter:\n%s", text)
	}
}

// TestFailureAroundTheRunWithNoCommitNamesTheWorktreeOnce is a regression test
// for BT-001's other composition.
//
// BT-001 fixed the double mention of the worktree path on describeRunOutcome's
// own non-success arms (OutcomeFailed/OutcomeTimedOut/OutcomeNoResult), leaving
// worktreeNote's call from outcomeView.withFailure untouched as "the legitimate
// single-mention case" — legitimate specifically when CommitSHA != "", per
// run_push_failed.golden.
//
// But withFailure calls worktreeNote unconditionally, on every one of its three
// branches, regardless of CommitSHA. metadataList's own Commit-line default arm
// separately names the worktree whenever the run did not succeed
// (view.succeed == false) and there is no commit (CommitSHA == "") but there is
// a branch to hang the line on. Every withFailure branch ends with
// view.succeed == false, so whenever a Run.Failure happens on top of a run that
// produced no commit at all — the worktree was never created, the run itself
// failed before committing, or the commit step (not just the push) is what
// failed — both call sites fire and the path is named twice.
func TestFailureAroundTheRunWithNoCommitNamesTheWorktreeOnce(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	tests := []struct {
		name string
		run  func() report.Run
	}{
		{
			name: "turn never reached the agent",
			run: func() report.Run {
				return report.Run{
					Ref:     testRef,
					Ordinal: 1,
					Failure: errors.New("git worktree add: fatal: 'lf/123-x' is already checked out somewhere"),
					Branch:  "lf/123-add-dry-run-to-task-clean",
				}
			},
		},
		{
			name: "run succeeded but the commit itself failed, not merely the push",
			run: func() report.Run {
				run := successRun()
				run.CommitSHA = ""
				run.SHAAfter = ""
				run.Pushed = false
				run.Failure = errors.New("git commit: fatal: unable to write new index file")
				return run
			},
		},
		{
			name: "run failed and cleanup afterwards also failed",
			run: func() report.Run {
				run := failureRun()
				run.Failure = errors.New("git worktree remove: fatal: could not remove worktree")
				return run
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := tt.run()
			run.WorktreePath = testWorktreePath

			body := render(t, rep, run).String()

			if got := strings.Count(body, testWorktreePath); got != 1 {
				t.Errorf("the report names the worktree %d times, want exactly 1:\n%s", got, body)
			}
		})
	}
}

// TestCostRowRendering is US-6 (T-6.2, AC-6.1): the Cost row is a
// HasCost/HasResult-driven three-way, and is never silently omitted.
func TestCostRowRendering(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	tests := []struct {
		name   string
		result *harness.RunResult
		want   string
	}{
		{
			name: "reported positive cost",
			result: &harness.RunResult{
				Outcome: harness.OutcomeSucceeded, HasResult: true, HasCost: true, CostUSD: 0.4213,
			},
			want: "- **Cost:** $0.4213",
		},
		{
			name: "reported exactly zero cost",
			result: &harness.RunResult{
				Outcome: harness.OutcomeSucceeded, HasResult: true, HasCost: true, CostUSD: 0,
			},
			want: "- **Cost:** $0.0000",
		},
		{
			name: "a terminal result with no cost reported at all",
			result: &harness.RunResult{
				Outcome: harness.OutcomeSucceeded, HasResult: true, HasCost: false,
			},
			want: "- **Cost:** unknown",
		},
		{
			name:   "no terminal result at all",
			result: nil,
			want:   "- **Cost:** unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := report.Run{Ref: testRef, Ordinal: 1, Branch: "lf/123-x", Result: tc.result}
			body := render(t, rep, run).String()

			if !strings.Contains(body, tc.want) {
				t.Errorf("the report is missing %q:\n%s", tc.want, body)
			}
			if !strings.Contains(body, "- **Cost:**") {
				t.Errorf("the Cost row is missing entirely:\n%s", body)
			}
		})
	}
}

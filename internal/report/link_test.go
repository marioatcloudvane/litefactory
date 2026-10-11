package report_test

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

// testBriefURL is the permalink successRun's report must carry: the task's own
// repository, the run's sha_after, and the derived path to the brief.
const testBriefURL = "https://github.com/acme/acme-api/blob/" +
	"9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1/" + testBriefPath

// TestTheReportLinksTheResearchBrief is the whole point of the link: the
// summary lands on the issue and the full brief is one click away.
//
// Three things are asserted rather than one, because each fails differently.
// The link has to be there; it has to be pinned to the commit this run
// produced, not to the branch, which would show whatever a later run wrote; and
// it has to sit in the metadata list, which is the section a truncated report
// never loses.
func TestTheReportLinksTheResearchBrief(t *testing.T) {
	t.Parallel()

	run := successRun()
	body := render(t, newReporter(t), run).String()

	want := "- **Research brief:** [`" + testBriefPath + "`](" + testBriefURL + ")"
	if !strings.Contains(body, want) {
		t.Errorf("the report does not link the brief.\nwant: %s\ngot:\n%s", want, body)
	}
	if !strings.Contains(body, "/blob/"+run.SHAAfter+"/") {
		t.Errorf("the link is not pinned to sha_after %q:\n%s", run.SHAAfter, body)
	}
	if strings.Contains(body, "/blob/"+run.Branch+"/") {
		t.Error("the link is branch-relative, so it does not show what this run produced")
	}

	// In the metadata list means in secFixed, the section runBody never trims.
	// A link that only survives on short reports is a link nobody can rely on.
	small, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Limit:    1_500,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	if body := render(t, small, run).String(); !strings.Contains(body, testBriefURL) {
		t.Errorf("a report trimmed to 1500 bytes lost the brief link:\n%s", body)
	}
}

// TestClaudeCommittedForItselfStillLinksARealSHA is the path a permalink built
// from the commit sha renders blank on.
//
// A run holding the Bash tool may commit for itself. git.Commit then reports
// Committed false and an empty SHA, while Head has moved and the branch is
// pushed — so there is real, published work and no commit sha to name it by.
// The link is pinned to sha_after precisely so that it still resolves here. A
// blank or missing sha in this test is the regression it exists to catch.
func TestClaudeCommittedForItselfStillLinksARealSHA(t *testing.T) {
	t.Parallel()

	const headAfter = "0badc0ffee1234567890abcdef1234567890abcd"

	run := successRun()
	run.CommitSHA = "" // git.CommitResult.SHA: nothing for litefactory to commit.
	run.SHAAfter = headAfter
	run.Pushed = true

	body := render(t, newReporter(t), run).String()

	if !strings.Contains(body, "/blob/"+headAfter+"/") {
		t.Errorf("no permalink pinned to the moved head %q:\n%s", headAfter, body)
	}
	if strings.Contains(body, "/blob//") {
		t.Error("the permalink has an empty sha, so it was built from CommitSHA")
	}
	if !strings.Contains(body, testBriefPath) {
		t.Errorf("the brief link is missing when the run committed for itself:\n%s", body)
	}
}

// TestAnUnpushedRunLinksNothing is the gate. A permalink into a commit that
// exists only in a local worktree is a guaranteed 404, and unpushed is not a
// rare failure: the branch not moving and an abandoned run are both ordinary,
// and a push that failed is the third.
//
// "No link" means no URL at all, not a link to something else. A report with a
// dead URL in it is worse than one without, because it invites the click.
func TestAnUnpushedRunLinksNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  func() report.Run
	}{
		{
			name: "the branch did not move, so nothing was pushed",
			run: func() report.Run {
				run := successRun()
				run.CommitSHA = ""
				run.Pushed = false
				run.Files = nil
				return run
			},
		},
		{
			name: "the run was abandoned while the daemon shut down",
			run: func() report.Run {
				run := successRun()
				run.Result = run.Result.Clone()
				run.Result.Outcome = harness.OutcomeAbandoned
				run.Result.ExitCode = harness.SignalExitCode
				run.Pushed = false
				return run
			},
		},
		{
			name: "the push failed after a good run",
			run: func() report.Run {
				run := successRun()
				run.Pushed = false
				run.Failure = errPushDenied
				run.WorktreePath = "/Users/mario/dev/.lf-worktrees/acme-api/task-123"
				return run
			},
		},
		{
			// The third of briefLink's three refusal gates (Pushed, SHAAfter,
			// BriefPath), exercised on its own: Pushed is true and BriefPath is
			// set, but the worktree was never inspected after the run, so there
			// is no sha to pin a permalink to. TestClaudeCommittedForItselfStillLinksARealSHA
			// covers the sibling case where SHAAfter is present and CommitSHA is
			// not; this is the one where SHAAfter itself is empty.
			name: "pushed, but sha_after was never recorded",
			run: func() report.Run {
				run := successRun()
				run.SHAAfter = ""
				return run
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := render(t, newReporter(t), tc.run()).String()

			for _, forbidden := range []string{"https://", "http://", "/blob/", "Research brief"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("an unpushed run's report contains %q:\n%s", forbidden, body)
				}
			}
		})
	}
}

// TestTheBriefLinkDoesNotDependOnWhatTheRunTouched is AC-B2.2c, and it is the
// reason discovery is a derivation rather than a scan of [report.Run.Files].
//
// The run that most needs the link is the one that read the brief and did not
// rewrite it — a plan or an implement run reporting against research somebody
// else's run wrote. Looking for the brief in the files-touched list would link
// it exactly when it is least needed and never when it is most.
func TestTheBriefLinkDoesNotDependOnWhatTheRunTouched(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files []git.FileChange
	}{
		{name: "the run changed nothing at all", files: nil},
		{
			name:  "the run changed code and never opened the brief",
			files: []git.FileChange{{Kind: git.ChangeModified, Path: "internal/cli/task.go"}},
		},
		{
			name:  "the run rewrote the brief",
			files: []git.FileChange{{Kind: git.ChangeModified, Path: testBriefPath}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := successRun()
			run.Files = tc.files

			if body := render(t, newReporter(t), run).String(); !strings.Contains(body, testBriefURL) {
				t.Errorf("the brief link is missing:\n%s", body)
			}
		})
	}

	// And the other half of the same property: the files-touched list cannot
	// conjure a link either. A run that rewrote the brief but never pushed it
	// has nothing on the remote to link to.
	run := successRun()
	run.Files = []git.FileChange{{Kind: git.ChangeModified, Path: testBriefPath}}
	run.Pushed = false
	if body := render(t, newReporter(t), run).String(); strings.Contains(body, "/blob/") {
		t.Errorf("an unpushed run that rewrote the brief still linked it:\n%s", body)
	}
}

// TestTheBriefURLIsBuiltFromTheTaskRef is AC-B2.2b's behavioural half: the
// owner and the repository in the URL come from [naming.Ref], which the report
// already carries, and from nowhere else.
func TestTheBriefURLIsBuiltFromTheTaskRef(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Ref = naming.Ref{Forge: naming.ForgeGitHub, Owner: "other-org", Repo: "other-repo", Issue: 123}

	body := render(t, newReporter(t), run).String()
	if !strings.Contains(body, "https://github.com/other-org/other-repo/blob/") {
		t.Errorf("the URL does not follow the task ref:\n%s", body)
	}
	// Narrowed to the URL: "acme/acme-api" is also in the local log path, which
	// the fixture keeps and which has nothing to do with where the link points.
	if strings.Contains(body, "github.com/acme/acme-api") {
		t.Errorf("the URL names a repository the ref does not:\n%s", body)
	}
}

// TestAnUnsupportedForgeIsNotGuessedAt covers the one branch that has no right
// answer. github.com's URL shape is not every forge's, so a ref on another
// forge gets no link rather than a link to the wrong host.
//
// The issue number is what makes this reachable at all: naming.Ref.Validate
// accepts any path-segment-shaped forge, so "gitlab" is a legal ref today even
// though nothing else in litefactory supports one.
func TestAnUnsupportedForgeIsNotGuessedAt(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.Ref = naming.Ref{Forge: "gitlab", Owner: "acme", Repo: "acme-api", Issue: 123}

	body := render(t, newReporter(t), run).String()
	if strings.Contains(body, "github.com") {
		t.Errorf("a non-github ref rendered a github.com URL:\n%s", body)
	}
	if strings.Contains(body, "/blob/") {
		t.Errorf("a forge with no known URL shape got a link anyway:\n%s", body)
	}
}

// TestRunCarriesNoForgeCoordinates is AC-B2.2b's structural half. The owner and
// the repository are already on [naming.Ref]; a second copy of either on
// [report.Run] would be a field two callers could set differently, and a URL
// built from the wrong one names a repository that is not the task's.
func TestRunCarriesNoForgeCoordinates(t *testing.T) {
	t.Parallel()

	forbidden := []string{"owner", "repo", "url", "host", "forge"}
	rt := reflect.TypeOf(report.Run{})
	for i := range rt.NumField() {
		name := strings.ToLower(rt.Field(i).Name)
		// Provider-returned ticket links are display values, not duplicate code
		// repository coordinates. They cannot select a delivery repository.
		if name == "sourceparenturl" || name == "sourcereplyurl" {
			continue
		}
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Errorf("report.Run has a field named %q; the forge coordinates belong to Ref alone",
					rt.Field(i).Name)
			}
		}
	}
}

// TestReportReachesNoForge is the import-level statement of the same rule, and
// of this package's own doc.go: it renders, it reaches no network, and it holds
// no forge client. Building the permalink from a ref is what makes that
// possible — importing internal/github for a base URL would put a client, a
// token and seven endpoints inside a package that promises none of them.
//
// internal/git and internal/harness are here because report takes their result
// types as render input; neither performs any I/O from this package.
func TestReportReachesNoForge(t *testing.T) {
	t.Parallel()

	forbidden := map[string]string{
		"github.com/cloudvane/litefactory/internal/github": "the forge client",
		"github.com/cloudvane/litefactory/internal/store":  "the database",
		"github.com/cloudvane/litefactory/internal/turn":   "the turn orchestrator, which depends on this package",
		"net/http": "the network",
		"os":       "the filesystem",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %s: %v", name, spec.Path.Value, err)
			}
			if why, bad := forbidden[path]; bad {
				t.Errorf("%s imports %q, which is %s; this package renders and nothing else", name, path, why)
			}
		}
	}
}

// TestAnOversizeSummaryIsStillPostedAndMarkedTruncated is the decision this
// story took about failing silent.
//
// A summary too large for a comment used to be able to end the turn with no
// comment at all: the render failed, nothing was posted, the run went
// unreported and the watermark did not move. For a feature whose whole purpose
// is visibility, "too big to show you, so you see nothing" is the wrong
// failure. Silence is the failing outcome here — every limit must produce a
// postable body that says, in words, that it was cut.
func TestAnOversizeSummaryIsStillPostedAndMarkedTruncated(t *testing.T) {
	t.Parallel()

	// Every explicit truncation notice this package or internal/redact can
	// leave. At least one has to be in the body, or a shortened comment is
	// indistinguishable from a complete one.
	notices := []string{
		"was cut here",
		"cut to fit the comment size limit",
		"Omitted to fit the comment size limit",
		"removed here to fit the comment size limit",
		"body truncated to fit the comment size limit",
	}

	for _, limit := range []int{600, 4_000, 16_000, github.CommentSizeLimit} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()

			rep, err := report.New(report.Options{
				Redactor: redact.New(redact.Options{Environ: []string{}}),
				Limit:    limit,
			})
			if err != nil {
				t.Fatalf("report.New: %v", err)
			}

			run := successRun()
			run.Result = run.Result.Clone()
			// A summary several times the size of any comment, on its own —
			// before the log tail, the files list or the metadata are counted.
			run.Result.ResultText = strings.Repeat(
				"The research brief covers the whole of the daemon's reporting path. ", 4_000)
			run.Output = numberedLines("worker: a line of run output", 200)

			body, err := rep.RenderRun(t.Context(), run)
			if err != nil {
				t.Fatalf("an oversize summary produced no comment at all: %v", err)
			}
			if !body.Valid() {
				t.Fatal("an oversize summary produced a Body that cannot be posted")
			}
			if body.Len() > limit {
				t.Errorf("body is %d bytes, over the %d-byte limit", body.Len(), limit)
			}

			text := body.String()
			if !strings.HasPrefix(text, report.StatusMarker(testRef, run.Ordinal)) {
				t.Errorf("the marker did not survive:\n%.200s", text)
			}
			marked := false
			for _, notice := range notices {
				if strings.Contains(text, notice) {
					marked = true
					break
				}
			}
			if !marked {
				t.Errorf("a truncated comment carries none of %q, so it reads as a complete one:\n%s",
					notices, text)
			}
		})
	}
}

// TestAgentMarkdownCannotBreakTheReport is the markup half of "the summary is
// untrusted".
//
// The run's final message is written by a model that read third-party issue
// comments, and it is the one part of the body rendered as markdown rather than
// quoted inside a fence — that is deliberate, because its formatting is part of
// the message. What that costs is a fence the model left open, which CommonMark
// runs to the end of the document: everything below the summary, including the
// log tail and its closing </details>, disappears into a code block.
func TestAgentMarkdownCannotBreakTheReport(t *testing.T) {
	t.Parallel()

	run := failureRun()
	run.Result = run.Result.Clone()
	run.Result.ResultText = strings.Join([]string{
		"I looked at the fence handling. Here is the offending snippet:",
		"",
		"```go",
		`fmt.Println("this fence is never closed")`,
		"</details>",
		"### not a heading",
		"\x1b[31mred\x1b[0m\x00",
	}, "\n")

	body := render(t, newReporter(t), run).String()

	if open := openFences(body); open != 0 {
		t.Errorf("the body leaves %d code fence(s) open, so everything below the summary is swallowed:\n%s",
			open, body)
	}
	if !strings.HasSuffix(strings.TrimRight(body, "\n"), "```") {
		t.Errorf("the body does not end with the fence [closeOpenFence] closed:\n%s", body)
	}

	// The report's own callout is still the report's: the agent's text sits
	// below it and cannot rewrite the line that says what happened.
	if !strings.Contains(body, "> [!CAUTION]\n> Run 3 failed") {
		t.Errorf("the status callout was not rendered intact:\n%s", body)
	}
	if headline(body) == "### not a heading" {
		t.Error("the summary's text became the report's heading")
	}

	// sanitize ran on it: the escapes and the NUL that make GitHub answer 422
	// are gone, and the readable text survived.
	for _, bad := range []string{"\x1b", "\x00", "[31m"} {
		if strings.Contains(body, bad) {
			t.Errorf("the summary carried %q into the body", bad)
		}
	}
	if !strings.Contains(body, "red") {
		t.Errorf("sanitize removed more than the escapes:\n%s", body)
	}
}

// TestAWellFormedReportLeavesNoFenceOpen is the control for the test above: the
// counter has to report zero for a body nobody attacked, or a zero there proves
// nothing.
func TestAWellFormedReportLeavesNoFenceOpen(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"success": render(t, newReporter(t), successRun()).String(),
		"failure": render(t, newReporter(t), failureRun()).String(),
	} {
		if open := openFences(body); open != 0 {
			t.Errorf("the %s report leaves a fence of %d backticks open:\n%s", name, open, body)
		}
	}
}

// openFences returns the length of the code fence a body leaves open, or zero
// when every fence in it is closed. It applies CommonMark's own rule: a fence
// opens on a line of three or more backticks with at most three spaces of
// indentation, and closes on a later line of at least as many backticks and
// nothing else.
func openFences(body string) int {
	open := 0
	for _, line := range strings.Split(body, "\n") {
		rest := strings.TrimLeft(line, " ")
		if len(line)-len(rest) > 3 {
			continue
		}
		marks := 0
		for marks < len(rest) && rest[marks] == '`' {
			marks++
		}
		if marks < 3 {
			continue
		}
		bare := strings.TrimRight(rest[marks:], " ") == ""
		switch {
		case open == 0:
			open = marks
		case bare && marks >= open:
			open = 0
		}
	}
	return open
}

// errPushDenied is the push failure the unpushed cases report against. What it
// wraps does not matter: internal/report words the callout from the message,
// and the gate under test reads [report.Run.Pushed], not the error.
var errPushDenied = errors.New("git push origin lf/123-add-dry-run-to-task-clean: permission denied (publickey)")

// TestABriefNamedOnAnEarlierDayIsStillLinked is the regression this file exists
// to hold onto.
//
// The brief link used to be derived from the run's own start day, on the
// assumption that a task finishes on the day its session directory was named.
// Issue #8 broke that assumption in the most ordinary way available: run 1 at
// 18:52 UTC on the 31st named the directory, run 2 at 06:10 UTC the next
// morning wrote the brief into it, and the report of run 2 linked a path with
// the next day's date in it — a 404, with the real 1072-line brief sitting one
// directory away. The comment was right that a brief existed and wrong about
// where, which sent its reader looking for a second artifact that had never
// been written.
//
// So the report is handed the path that was found on disk, and the run's own
// clock has nothing to do with it. This asserts the case the derivation got
// wrong: a run days later still links the directory that exists.
func TestABriefNamedOnAnEarlierDayIsStillLinked(t *testing.T) {
	t.Parallel()

	run := successRun()
	// A month after the session directory in testBriefPath was named. Under the
	// old derivation this rendered ".../2026-06-19/..." and 404ed.
	run.Result.StartedAt = time.Date(2026, 6, 19, 9, 30, 0, 0, time.UTC)
	run.Result.FinishedAt = run.Result.StartedAt.Add(time.Minute)

	body := render(t, newReporter(t), run).String()
	if !strings.Contains(body, testBriefPath) {
		t.Errorf("the report did not link the brief that exists:\n%s", body)
	}
	if strings.Contains(body, "2026-06-19") {
		t.Errorf("the link carries the run's own day, so it is derived again:\n%s", body)
	}
}

// TestARunWithNoBriefLinksNothing covers the other half: an empty BriefPath
// means the caller looked and found none, and inventing a plausible path for it
// is exactly the behaviour that produced the 404 above.
func TestARunWithNoBriefLinksNothing(t *testing.T) {
	t.Parallel()

	run := successRun()
	run.BriefPath = ""

	body := render(t, newReporter(t), run).String()
	if strings.Contains(body, "Research brief") {
		t.Errorf("a run with no brief still advertised one:\n%s", body)
	}
	if strings.Contains(body, "RESEARCH_BRIEF.md") {
		t.Errorf("a run with no brief still named one:\n%s", body)
	}
}

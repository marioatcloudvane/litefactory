package report

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
)

// defaultAgentName is the agent named in a run report when [Run.Agent] was
// never set. It exists so the many call sites — most of them in tests —
// written before this package took an Agent field at all keep reading as a
// Claude run, which is every run they describe.
const defaultAgentName = "claude"

// TailLines is how many lines of run output the <details> block carries
// (AC-4.3, S-9). It is exported so the caller capturing that output knows how
// much of it is worth keeping.
const TailLines = 40

const (
	// maxResultBytes caps the run's final message. A model can emit a message
	// far larger than a comment, and the summary is the part that must never be
	// the reason a body is over the limit, so it is bounded on the way in
	// rather than trimmed as an emergency later.
	maxResultBytes = 32 << 10

	// maxFileEntries caps the files-touched list. A refactor touching two
	// thousand files produces a list nobody reads and a body nobody can post;
	// the count above the list still tells the true number.
	maxFileEntries = 200

	// maxFenceBytes is the room [resultSection] reserves for a closing fence
	// [closeOpenFence] may have to append after a cut. A run of more than 64
	// backticks in a final message is not worth sizing anything for, and
	// [runBody]'s budget pass is the limit that actually has to hold.
	maxFenceBytes = 64

	// minTailBytes is the least amount of room worth giving the log tail. Below
	// this the block is dropped whole and replaced with a line naming the local
	// log, because a dozen surviving bytes of output is not evidence of
	// anything.
	minTailBytes = 512
)

// Run is everything [Reporter.RenderRun] needs to render one run report.
//
// It is assembled by the caller from the run's row, its worktree and its
// harness result. This package deliberately takes none of those types wholesale
// — it holds no database handle and does no git or forge work — so a report is
// renderable in a test from four literals.
//
// Every field except Ref and Ordinal is allowed to be absent. A run that
// crashed on its way to reporting still gets a comment (AC-4.1).
type Run struct {
	// Meter contains prepared frozen metering values, independent of legacy cost.
	Meter            *MeterSummary
	Recovery         *Recovery
	SourceStoryIssue int
	SourceCommentID  int64
	// Ref is the task this run belongs to. It appears in the marker, so it must
	// validate.
	Ref naming.Ref

	// Ordinal is the run's number within its session — the N in run=N and the
	// NNNN in the log filename. Must be 1 or more.
	Ordinal int

	// Agent is which coding-agent tool ran this — "claude" or "codex" — and
	// it is what lets the callout name the tool that actually ran rather
	// than assuming one. Empty falls back to [defaultAgentName]: every
	// caller before this field existed described a Claude run, and none of
	// them has to be taught about it just to keep reading correctly.
	Agent string

	// Result is what the runner recorded. Nil means litefactory never got a
	// result back, which is itself reportable: the run is rendered with an
	// unknown outcome rather than silently as a success.
	Result *agent.RunResult

	// Branch is the git branch the run worked on, for example "lf/123-add-x".
	//
	// It is rendered as plain text — the branch is a fact about where the work
	// is, not a destination — but it is not inert: the feature slug frozen into
	// it is what [Run.briefLink] recovers to derive the session directory, so a
	// branch that is not one [naming.Branch] built is the difference between a
	// report that links its brief and one that does not.
	Branch string

	// CommitSHA is the commit this run produced, empty when it committed
	// nothing (AC-3.23). Plain text: it names the commit, and [Run.SHAAfter] is
	// what anything clickable is pinned to.
	CommitSHA string

	// SHAAfter is where the branch points after the run — the run's sha_after,
	// and the commit every link in this report is pinned to.
	//
	// It is a separate field from CommitSHA because the two genuinely differ.
	// claude runs with the Bash tool and may commit for itself, which leaves
	// CommitSHA empty while the branch has moved (see git.CommitResult.Head).
	// A permalink built from CommitSHA would therefore render blank on exactly
	// the supported path where the work is real, so it is built from this.
	//
	// Empty when the worktree was never inspected after the run, in which case
	// no link is rendered at all.
	SHAAfter string

	// Pushed reports whether the branch reached the remote. A commit that exists
	// only in the local worktree is a materially different thing to report, so
	// the two are not conflated — and it is what gates every link in the body,
	// because a permalink into an unpushed commit is a guaranteed 404.
	Pushed bool

	// Files is the files-touched list from the worktree's status: a plain
	// git-derived list of the run's changes plus its untracked additions
	// (AC-4.4). It is not an aiteam artefact walk — that is out of scope at M0.
	Files []git.FileChange

	// Output is the run's output as captured locally: stream text, stderr, or
	// both. Only the last [TailLines] lines of it reach the comment, and the
	// unredacted whole of it stays in $LF_HOME/logs. Empty falls back to the
	// harness result's stderr tail.
	Output string

	// LogPath is the absolute path of the run's local log. Empty falls back to
	// the harness result's own, and it is set separately only for a turn that
	// has a log but no result to read it from.
	LogPath string

	// Failure is a failure that was not the run's own: a worktree that could
	// not be created, a commit or a push that did not complete. It is what
	// separates "litefactory never got as far as running this" from "the run
	// itself failed", and the two need different things from their reader.
	//
	// A Failure alongside a successful run is the push-failure path: the work
	// exists and is committed, and the report has to say so and say where,
	// rather than presenting a success nobody can find on the remote.
	Failure error

	// WorktreePath is where the work is on this machine. It is quoted when a
	// push failed, because that is where the commit still is (AC-3.24).
	WorktreePath string

	// ToolsRequested is the tool surface litefactory asked claude for, and
	// ToolProfile is the name of the operator-authored profile that surface came
	// from — empty when no profile was selected.
	//
	// They are on Run rather than on Result because they are known before the
	// subprocess starts and must survive a run that produced no result at all:
	// Result is nil for a run that died before the init event, which is exactly
	// the run whose requested surface is worth reading. The resolved surface —
	// what claude actually ran under — stays on Result, where the init event put
	// it, and the two are reported side by side.
	//
	// ToolProfile is a validated profile name from the operator's own config. No
	// text originating from the forge is ever carried here.
	ToolsRequested []string
	ToolProfile    string

	// Sync is what litefactory's automatic branch sync reached for this run's
	// task branch, before the run itself started (US-2). Nil renders no sync
	// section at all — a caller that does not populate this field, or a repo
	// where auto-sync has not landed, reports exactly as it did before this
	// field existed. A non-nil Sync whose Outcome is [SyncUpToDate] also
	// renders nothing (AC-3.1): the branch already had everything the base
	// did, and that is not itself something to disclose.
	//
	// Every other outcome gets a rendered sentence, [SyncSkipped] included —
	// a skip is never silent (AC-3.2), whatever caused it.
	Sync *Sync

	// BriefPath is the repository-relative, slash-separated path of this task's
	// RESEARCH_BRIEF.md, as found on disk by the caller. Empty means the caller
	// looked and there is no brief — or had no worktree to look in — and the
	// report renders no brief link at all.
	//
	// It is discovered rather than derived, and that is the whole point. This
	// package cannot do the discovering: the path carries the date the session
	// directory was named on, which is knowable only by looking, and looking is
	// I/O this package promises not to do. Deriving it from the run's own start
	// day is what it used to do, and it was wrong for every run of a task that
	// outlived the day its session directory was created — it linked a path
	// that had never existed while the brief sat one directory away. A report
	// that names no brief is recoverable; one that names the wrong brief is not.
	BriefPath string
}

// agentName returns which coding-agent tool ran this, falling back to
// [defaultAgentName] for a Run built before this field existed.
func (r Run) agentName() string {
	if r.Agent == "" {
		return defaultAgentName
	}
	return r.Agent
}

// logPath returns the absolute path of the run's local log, or "" if it was
// never recorded.
func (r Run) logPath() string {
	if r.LogPath != "" {
		return r.LogPath
	}
	if r.Result == nil {
		return ""
	}
	return r.Result.LogPath
}

// logsCommand is the command that shows this run's log on the machine that ran
// it. It is what the comment points at: the comment is redacted and size-capped
// by design, and this is how a reader gets the whole of what it left out.
func (r Run) logsCommand() string {
	return fmt.Sprintf("litefactory logs %d --run %d", r.Ref.Issue, r.Ordinal)
}

// outputText returns the run output to quote, falling back to the stderr tail
// the harness kept when the caller captured nothing else. A failed run whose
// only trace is what it printed to stderr is exactly the run S-9 is about.
func (r Run) outputText() string {
	if strings.TrimSpace(r.Output) != "" {
		return r.Output
	}
	if r.Result != nil {
		return r.Result.Stderr
	}
	return ""
}

// duration returns the run's wall-clock duration and whether it is known.
func (r Run) duration() (time.Duration, bool) {
	res := r.Result
	if res == nil {
		return 0, false
	}
	if res.Duration > 0 {
		return res.Duration, true
	}
	if !res.StartedAt.IsZero() && res.FinishedAt.After(res.StartedAt) {
		return res.FinishedAt.Sub(res.StartedAt), true
	}
	return 0, false
}

// cuts records what the fitting pass had to remove, for the log line that
// explains a short comment.
type cuts struct {
	output        bool
	outputDropped bool
	result        bool
	filesDropped  bool
}

func (c cuts) any() bool {
	return c.output || c.outputDropped || c.result || c.filesDropped
}

// Section indices into the assembled body. The order is the reading order and
// also the survival order: everything above the log tail outranks it, so even
// [redact.Redactor.Apply]'s own last-resort cut from the bottom takes the
// expendable part first.
const (
	secFixed  = iota // marker, heading, status callout, metadata — never cut
	secSync          // base branch sync disclosure — never cut (US-3)
	secResult        // the run's final message
	secFiles         // files touched
	secOutput        // the <details> log tail
	numSections
)

// sectionGap separates two sections of the body.
const sectionGap = "\n\n"

// runBody assembles the report and trims it to the budget, reporting what it
// had to remove.
//
// The trimming order is AC-4.8's: the log tail goes first and from the middle,
// because the whole of it is on disk and the body says where. Only if removing
// it outright still leaves the body over budget — which the caps on the result
// text and the files list make close to unreachable — does anything in the
// summary get touched.
func (r *Reporter) runBody(run Run) (string, cuts) {
	var c cuts
	view := describeOutcome(run)
	logPath := run.logPath()

	var sections [numSections]string
	sections[secFixed] = fixedSection(run, view)
	sections[secSync] = syncSection(run)
	sections[secResult] = resultSection(run, view)
	sections[secFiles] = filesSection(run)

	output := lastLines(sanitize(run.outputText()), TailLines)
	fence := fenceFor(output)
	sections[secOutput] = outputSection(output, fence, false)

	if body := join(sections[:]); len(body) <= r.budget {
		return body, c
	}

	// 1. The log tail: elided from the middle, or dropped whole if what would
	//    survive is not worth the space. Skipped when there was no output to
	//    quote in the first place — "omitted" and "there was none" are
	//    different facts and the report must not confuse them.
	if output != "" {
		sections[secOutput], c.output, c.outputDropped =
			fitOutput(output, fence, logPath, r.roomFor(sections, secOutput))
	}
	if body := join(sections[:]); len(body) <= r.budget {
		return body, c
	}

	// 2. The run's final message, cut from the end: its opening is the part
	//    that says what happened.
	if sections[secResult] != "" {
		if room := r.roomFor(sections, secResult); room > len(resultHeading)+minTailBytes {
			sections[secResult], c.result = truncateEnd(sections[secResult], resultCutNotice(logPath), room)
		} else {
			sections[secResult] = droppedResultNotice(logPath)
			c.result = true
		}
	}
	if body := join(sections[:]); len(body) <= r.budget {
		return body, c
	}

	// 3. Last: the files-touched list. Reaching here means the fixed section
	//    alone is near the budget, which takes a pathological log path or task
	//    ref; Apply's hard-truncate is the backstop below this.
	sections[secFiles] = filesHeading + sectionGap + "_Omitted to fit the comment size limit._"
	c.filesDropped = true
	// Supplemental guidance must not push mandatory metadata (especially the
	// sole worktree location) into Apply's hard truncation. Rebudget with shorter
	// guidance when even the reduced report cannot fit. Clone keeps rendering
	// pure; each retry removes at least the overflow plus literal markup room,
	// and guidance too short to be useful is omitted altogether.
	if len(join(sections[:])) > r.budget && run.Result != nil &&
		run.Result.Outcome == agent.OutcomeFailed && run.Result.Diagnostic != "" {
		run.Result = run.Result.Clone()
		diagnostic := oneLine(run.Result.Diagnostic)
		room := min(len(diagnostic), 1024) - (len(join(sections[:])) - r.budget) - 32
		if room < 64 {
			run.Result.Diagnostic = ""
		} else {
			run.Result.Diagnostic, _ = truncateEnd(diagnostic, "…", room)
		}
		return r.runBody(run)
	}
	return join(sections[:]), c
}

// fitOutput cuts the log tail down to room bytes, or gives up on it entirely.
// It reports whether the tail was elided and whether it was dropped.
//
// Giving up is a real outcome rather than a failure: a hundred surviving bytes
// of a log is not evidence of anything, and the line that replaces the block
// says where the whole of it is.
func fitOutput(output, fence, logPath string, room int) (section string, elided, dropped bool) {
	content := room - outputOverhead(fence, true)
	if room >= minTailBytes && content >= minTailBytes {
		if cut, ok := elideMiddle(output, content, elisionNotice(logPath)); strings.TrimSpace(cut) != "" {
			return outputSection(cut, fence, ok), ok, false
		}
	}
	return droppedOutputNotice(logPath), false, true
}

// roomFor returns how many bytes section i may occupy for the whole body to sit
// inside the budget.
func (r *Reporter) roomFor(sections [numSections]string, i int) int {
	sections[i] = ""
	return r.budget - len(join(sections[:])) - len(sectionGap)
}

// join concatenates the non-empty sections, separated by a blank line.
func join(sections []string) string {
	present := make([]string, 0, len(sections))
	for _, s := range sections {
		if s != "" {
			present = append(present, s)
		}
	}
	return strings.Join(present, sectionGap) + "\n"
}

// outcomeView is how one outcome is presented: the word in the heading, the
// GitHub alert kind for the callout, and the sentence that says what to do
// about it.
//
// The five outcomes are presented as five different things on purpose. A
// timeout is not a crash, and an abandonment is neither: it means the daemon
// was stopping, so nothing is wrong with the task and nobody should go hunting.
type outcomeView struct {
	word    string
	alert   string
	lead    string
	succeed bool
}

// describeOutcome renders the heading word and the callout for a run, taking
// account of a failure that happened around the run as well as of how the run
// itself ended.
func describeOutcome(run Run) outcomeView {
	view := describeRunOutcome(run)
	if run.Failure == nil {
		return view
	}
	return view.withFailure(run)
}

// withFailure folds a failure that was not the run's own into the callout.
//
// The three cases are different reports, not one report with a footnote. A turn
// that never reached `claude` has no run to describe; a run that worked but
// could not be pushed is not a success, because nothing a reader can fetch came
// of it; and a run that failed and then could not be tidied up needs both facts
// in the order they happened.
func (v outcomeView) withFailure(run Run) outcomeView {
	failure := strings.TrimSpace(sanitize(run.Failure.Error()))

	switch {
	case run.Result == nil:
		v = outcomeView{
			word:  "did not run",
			alert: "CAUTION",
			lead:  fmt.Sprintf("litefactory could not start coding agent %s for run %d: %s", codeSpan(run.agentName()), run.Ordinal, failure),
		}
	case v.succeed:
		v = outcomeView{
			word:  "not completed",
			alert: "CAUTION",
			lead: fmt.Sprintf("Run %d did its work, but litefactory could not complete the turn: %s",
				run.Ordinal, failure),
		}
	default:
		v.lead += "\n\nlitefactory then could not complete the turn either: " + failure
	}

	// Only mention the worktree here when there is a commit to distinguish
	// this callout from: with CommitSHA != "", metadataList's Commit line
	// takes its own case (line 567/569) and never names the path itself, so
	// this is the single place that does. With CommitSHA == "", metadataList's
	// Commit-line default arm (below) already names the path for a non-success
	// run with nothing committed — naming it here too would say it twice.
	if run.CommitSHA != "" {
		v.lead += worktreeNote(run.WorktreePath)
	}
	return v
}

// worktreeNote is the paragraph naming where a run's unfinished work still
// sits, appended to a callout describing a run that did not succeed. Empty
// when the worktree path was never recorded, because a report should never
// claim a location it does not have.
//
// Called only from [outcomeView.withFailure], for a failure that happened
// around the run itself (Run.Failure != nil) and left a commit behind
// (Run.CommitSHA != ""), not from [describeRunOutcome]'s own non-success arms
// and not for a CommitSHA == "" failure — those defer to metadataList's
// Commit line, the single place a non-success report with nothing committed
// names the worktree path (AC-3.1).
func worktreeNote(path string) string {
	if path == "" {
		return ""
	}
	return "\n\nWhatever the run produced is still in the worktree" + inWorktreeAt(path) + "."
}

// inWorktreeAt renders where a run's worktree sits, as a clause to append
// after "the worktree" — the path, quoted through [codeSpan] so it cannot
// break the comment's markdown, or nothing when the path was never recorded.
func inWorktreeAt(path string) string {
	if path == "" {
		return ""
	}
	return " at " + codeSpan(path)
}

// describeRunOutcome renders the heading word and the callout for how the run
// itself ended.
//
// The callout is prose: what happened, and what the person reading it can do
// about it. The harness's own one-line reason — "claude exited with status 1",
// "timed out after 30m0s; the process subtree was killed" — is not repeated
// here. It has one home, the Reason entry of the metadata list, which is what
// S-9 asks for and what keeps the callout from reading as the same sentence
// twice.
func describeRunOutcome(run Run) outcomeView {
	n := run.Ordinal
	res := run.Result
	if res == nil {
		return outcomeView{
			word:  "outcome unknown",
			alert: "CAUTION",
			lead: fmt.Sprintf("litefactory did not record how run %d ended — it was most likely interrupted "+
				"between finishing the run and reporting on it. The local log is the authority on what happened.", n),
		}
	}

	tool := run.agentName()

	switch res.Outcome {
	case agent.OutcomeSucceeded:
		return outcomeView{word: "succeeded", succeed: true}

	case agent.OutcomeInterrupted:
		return outcomeView{word: "interrupted by session limit", alert: "WARNING", lead: fmt.Sprintf("Run %d was interrupted by the coding agent session limit. The feature is not complete.", n)}

	case agent.OutcomeFailed:
		lead := fmt.Sprintf("Run %d failed: `%s` exited with status %d.", n, tool, res.ExitCode)
		if res.ExitCode == agent.SignalExitCode {
			lead = fmt.Sprintf("Run %d failed without an exit status of its own.", n)
		}
		// Guidance is a neutral adapter value, not arbitrary Reason content.
		// Quote it literally and cap it before it enters the fixed report budget.
		if diagnostic := oneLine(res.Diagnostic); diagnostic != "" {
			diagnostic, _ = truncateEnd(diagnostic, "…", 1024)
			lead += "\n\nDiagnostic: " + codeSpan(diagnostic)
		}
		return outcomeView{word: "failed", alert: "CAUTION", lead: lead}

	case agent.OutcomeTimedOut:
		lead := fmt.Sprintf("Run %d ran past the configured timeout for `%s` and litefactory killed it", n, tool)
		if d, ok := run.duration(); ok {
			lead += " after " + formatDuration(d)
		}
		lead += ". The work it had done up to that point is still in the worktree. " +
			"Raise the timeout or narrow the task, then comment again."
		return outcomeView{word: "timed out", alert: "CAUTION", lead: lead}

	case agent.OutcomeAbandoned:
		lead := fmt.Sprintf("Run %d was abandoned because litefactory was shutting down. "+
			"Nothing failed and nothing crashed: the daemon was asked to stop while the run was in flight, "+
			"so it stopped. Comment again to run it.", n)
		return outcomeView{word: "abandoned", alert: "NOTE", lead: lead}

	case agent.OutcomeNoResult:
		lead := fmt.Sprintf("Run %d exited cleanly without ever reporting a result. "+
			"In practice this is usually a stale `%s` credential: the tool retries internally about ten times "+
			"and then goes quiet. Run `%s` once by hand to check it is still logged in, then comment again.", n, tool, tool)
		return outcomeView{word: "no result", alert: "WARNING", lead: lead}

	default:
		return outcomeView{
			word:  "outcome unrecognised",
			alert: "CAUTION",
			lead: fmt.Sprintf("Run %d finished with an outcome litefactory does not recognise (%s). "+
				"This is a bug in litefactory, not in the task. The local log is the authority.", n, res.Outcome),
		}
	}
}

// fixedSection renders the part of the body that is never cut: the marker, the
// heading, the status callout, the metadata list and [ReplyPlainlyNotice]. The
// absolute log path is in here rather than beside the output it describes,
// precisely so that it survives every level of truncation (AC-4.5, AC-4.8).
//
// The notice is in here, and not appended to the body, for the same reason.
// Everything below the log tail is what redact.Apply's last-resort hard truncate
// eats first, and a report that cost a run to produce should not be the one that
// arrives without the line saying how to answer it (AC-S0.6).
func fixedSection(run Run, view outcomeView) string {
	var b strings.Builder
	b.WriteString(StatusMarker(run.Ref, run.Ordinal))
	b.WriteString(storySource(run.Ref, run.SourceStoryIssue, run.SourceCommentID))
	b.WriteString(sectionGap)
	fmt.Fprintf(&b, "### litefactory · run %d · %s", run.Ordinal, view.word)
	if view.alert != "" {
		b.WriteString(sectionGap)
		b.WriteString(alertBlock(view.alert, view.lead))
	}
	b.WriteString(sectionGap)
	b.WriteString(metadataList(run, view))
	b.WriteString(meterMetadata(run.Meter))
	b.WriteString(sectionGap)
	b.WriteString(ReplyPlainlyNotice)
	if text := recoverySection(run.Recovery); text != "" {
		b.WriteString(sectionGap)
		b.WriteString(text)
	}
	return b.String()
}

// alertBlock renders a GitHub alert — the callout that makes a failure report
// distinguishable from a success one at a glance (AC-4.5) without depending on
// anyone reading the heading. It degrades to an ordinary blockquote anywhere
// that does not render alerts.
func alertBlock(kind, text string) string {
	var b strings.Builder
	b.WriteString("> [!")
	b.WriteString(kind)
	b.WriteString("]\n")
	for line := range strings.SplitSeq(text, "\n") {
		if line == "" {
			// A bare ">" rather than "> ": a blank quoted line keeps the
			// paragraph inside the callout without leaving trailing whitespace
			// in the body.
			b.WriteString(">\n")
			continue
		}
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// metadataList renders the facts of the run as a bullet list. Every entry is
// omitted rather than guessed when it is not known.
//
// view is the outcome already computed for the callout above — passed in
// rather than recomputed, so the Commit line's wording and the callout's
// never disagree about whether this run succeeded.
func metadataList(run Run, view outcomeView) string {
	var lines []string
	add := func(label, value string) {
		lines = append(lines, "- **"+label+":** "+value)
	}

	if run.Branch != "" {
		add("Branch", codeSpan(run.Branch))
	}
	switch {
	case run.CommitSHA != "" && run.Pushed:
		add("Commit", codeSpan(run.CommitSHA)+" (pushed)")
	case run.CommitSHA != "":
		add("Commit", codeSpan(run.CommitSHA)+" (in the local worktree only — not pushed)")
	case run.Branch == "":
		// Nothing to report: with no branch there is no worktree to name either.
	case view.succeed:
		add("Commit", "none — this run committed nothing")
	default:
		// A non-success run that committed nothing is not the same fact as a
		// successful run that had nothing to change (AC-3.1): the words must
		// differ, and this is where the reader is told where the work — if
		// there is any — still is.
		add("Commit", "none — this run did not succeed; if it produced anything, it is still in the worktree"+
			inWorktreeAt(run.WorktreePath))
	}

	// The brief sits with the branch and the commit because it is the same kind
	// of fact — where the work is — and because being in metadataList puts it in
	// secFixed, the one section [runBody] never trims. A link that only survives
	// on short comments is a link nobody can rely on.
	//
	// The caveat is on the line rather than in this comment on purpose. The path
	// is derived and not discovered (see [Run.briefLink]), so the report must not
	// present it as a page it has checked is there.
	if link, ok := run.briefLink(); ok {
		add("Research brief", link+" — pinned to this run's commit; 404s until a Research run writes one")
	}

	if res := run.Result; res != nil {
		switch res.ExitCode {
		case agent.SignalExitCode:
			add("Exit status", "killed by litefactory, so there is no exit code")
		default:
			add("Exit status", codeSpan(strconv.Itoa(res.ExitCode)))
		}
		// The kill reason S-9 requires alongside the exit code, and its only
		// home in the body: the callout above is prose, this is the line the
		// harness actually recorded.
		if reason := oneLine(res.Reason); reason != "" {
			add("Reason", reason)
		}
	} else {
		add("Exit status", "unknown")
	}

	if d, ok := run.duration(); ok {
		add("Duration", formatDuration(d))
	} else {
		add("Duration", "unknown")
	}

	if res := run.Result; res != nil {
		switch {
		case res.HasCost:
			label := "Cost"
			if run.Meter != nil {
				label = "Legacy reported cost"
			}
			add(label, fmt.Sprintf("$%.4f", res.CostUSD))
		default:
			add("Cost", "unknown")
		}
		if res.Model != "" {
			add("Model", codeSpan(res.Model))
		}
	} else {
		add("Cost", "unknown")
	}

	if tools, ok := toolsEntry(run); ok {
		add("Tools", tools)
	}

	// The command first, the path second. A path to a JSONL file is a thing to
	// go and find a way to read; the command is the way to read it, and it is
	// the same command whether or not the run recorded a path, because `logs`
	// derives one from the task ref when the row carries none.
	if path := run.logPath(); path != "" {
		add("Log", codeSpan(run.logsCommand())+" — the file is "+codeSpan(path)+
			" (unredacted, on the machine that ran it)")
	} else {
		add("Log", codeSpan(run.logsCommand())+" (no local log path was recorded for this run)")
	}

	if res := run.Result; res != nil && res.StreamError != nil {
		add("Note", "the run's output could not be captured in full: "+sanitize(res.StreamError.Error()))
	}

	return strings.Join(lines, "\n")
}

// toolsEntry renders the Tools bullet: the surface the run actually had, the
// profile that surface came from, and anything litefactory asked for and did
// not get. The second result is false only when nothing about the run's tools
// was recorded at all, which is metadataList's standing rule — omit rather than
// guess — applied to a run that never reached a boundary decision.
//
// This line is required, not decorative. Do not make it conditional and do not
// drop it to save a bullet:
//
//   - Profiles are resolved per tick, not per comment: two comments drained in
//     one tick share one label snapshot. This bullet is the only after-the-fact
//     evidence of which snapshot the run in front of you used.
//   - Labels are authenticated by nothing (C3). Anyone with triage permission
//     can widen an issue's profile and wait for the next comment. The ceiling
//     bounds what that can reach; this line is the only thing that makes it
//     visible afterwards, so it is the whole detection story for a relabel.
//   - Requested and resolved genuinely differ in practice. TodoWrite was
//     requested and silently dropped for the life of the project — eight asked
//     for, seven granted, no error anywhere — and nothing but this comparison
//     would have shown it.
func toolsEntry(run Run) (string, bool) {
	var resolved []string
	if run.Result != nil {
		resolved = run.Result.Tools
	}
	requested := run.ToolsRequested

	if len(resolved) == 0 && len(requested) == 0 && run.ToolProfile == "" {
		return "", false
	}

	var parts []string
	switch {
	case len(resolved) > 0:
		parts = append(parts, codeList(resolved))
	case len(requested) > 0:
		// No init event arrived, so the surface claude ran under was never
		// observed. Say what was asked for and say plainly that it is not the
		// same claim (AC-4.3).
		parts = append(parts, "requested "+codeList(requested)+
			", but the run recorded no resolved surface")
	default:
		parts = append(parts, "no tool surface was recorded")
	}

	if run.ToolProfile != "" {
		parts = append(parts, "profile "+codeSpan(run.ToolProfile))
	} else {
		// A run with no profile ran on the operator's configured surface. That
		// is a fact about the run, not an absence, and it is what every run
		// before profiles existed will say.
		parts = append(parts, "no profile (the operator's default surface)")
	}

	// Only worth computing when both lists are real: with no resolved surface
	// to compare against, every requested tool would look dropped.
	if len(resolved) > 0 {
		if missing := notGranted(requested, resolved); len(missing) > 0 {
			parts = append(parts, "requested but not granted: "+codeList(missing))
		}
	}

	return strings.Join(parts, " — "), true
}

// notGranted is the requested tools that the resolved surface does not carry,
// in the order they were requested.
func notGranted(requested, resolved []string) []string {
	var missing []string
	for _, tool := range requested {
		if !slices.Contains(resolved, tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}

// codeList renders names as a comma-separated list of code spans.
func codeList(names []string) string {
	spans := make([]string, 0, len(names))
	for _, name := range names {
		spans = append(spans, codeSpan(name))
	}
	return strings.Join(spans, ", ")
}

// SyncOutcome is which of the outcomes litefactory's automatic branch
// sync reached for one run's task branch, before the run itself started
// (US-2).
//
// It is this package's own string type rather than a reuse of internal/turn's
// unexported syncOutcome — the same reason [ReplyKind] is its own enum
// instead of internal/turn's CommandKind: internal/turn depends on this
// package to render its reports, so the dependency cannot point the other
// way. The string values are chosen to match internal/turn's exactly, so a
// future caller can convert one to the other with a plain type conversion
// rather than a switch.
type SyncOutcome string

const (
	// SyncUpToDate means the branch already had everything the base branch
	// did. [Run.Sync] carrying this outcome renders no sync section at all
	// (AC-3.1) — there is nothing to disclose.
	SyncUpToDate SyncOutcome = "up_to_date"

	// SyncSynced means litefactory merged the base branch in cleanly before
	// the run started.
	SyncSynced SyncOutcome = "synced"

	// SyncBlocked means a merge was attempted only far enough to discover it
	// would conflict; the branch was left exactly as it was, unmerged, and
	// the run went ahead against it. This is the normal-run disclosure —
	// not the refusal a different, factory-owned mechanism sends when an
	// explicit sync request cannot be honoured (US-4's ReplyRefusesSync).
	SyncBlocked SyncOutcome = "blocked"

	// SyncSkipped means litefactory chose not to attempt a sync it could
	// have attempted — the configured cap on how far behind a branch may be,
	// auto-sync turned off for the repo, or an `Sync: Off` label on the
	// issue. [Sync.Reason] is what says which; a skip is never rendered
	// silently (AC-3.2).
	SyncSkipped SyncOutcome = "skipped"

	// SyncDeferredDirty means retained edits prevented synchronization; the run
	// proceeded on the current branch without modifying them during sync.
	SyncDeferredDirty SyncOutcome = "deferred_dirty"

	// SyncFailed means litefactory tried to sync and the attempt itself
	// broke — as distinct from [SyncBlocked], where nothing was attempted
	// because the conflict was foreseen ahead of time.
	SyncFailed SyncOutcome = "failed"
)

// Sync is what litefactory's automatic branch sync reached for one run's task
// branch, before the run itself started. See [Run.Sync] for how a nil or
// up-to-date value renders.
type Sync struct {
	// Outcome is the synchronization decision reached.
	Outcome SyncOutcome

	// Behind and Ahead are the divergence from Base that was measured before
	// Outcome was decided: how many commits Base had that the branch did
	// not, and how many the branch had that Base did not.
	Behind, Ahead int

	// Base is the base ref sync compared against and, when Outcome is
	// [SyncSynced], the ref that was merged in — for example
	// "origin/main". Rendered as plain text.
	Base string

	// CommitSHA is the merge commit sync produced, non-empty only when
	// Outcome is [SyncSynced] and the merge actually moved the branch.
	CommitSHA string

	// Conflicts lists the paths a blocked merge preview reported, exactly as
	// given. It is rendered inside a fenced block, the same defence
	// [filesSection] uses for the files-touched list, so a path cannot
	// become markup in the comment (S-2) no matter what it contains — but
	// this package does not sanitise or truncate the list itself, or decide
	// which paths are safe to show at all. That is a caller's job (US-5);
	// this renderer shows whatever list it is handed.
	Conflicts []string

	// Reason is why sync did not complete, in a sentence fit to render
	// directly after "because ": why it was skipped (the configured cap,
	// auto-sync being off, or an `Sync: Off` label) when Outcome is
	// [SyncSkipped], or what went wrong when Outcome is [SyncFailed]. Empty
	// still renders a sentence rather than nothing — a skip is never silent
	// (AC-3.2) even when the caller did not record why.
	Reason string
}

const syncHeading = "**Base branch sync**"

// syncSection renders the base-branch-sync disclosure (US-3). A nil Sync, or
// one whose Outcome is [SyncUpToDate], renders nothing at all (AC-3.1): a
// section that is merely empty would still look like something was omitted,
// and the point of this rendering nothing here is that there is nothing to
// say.
//
// Every other outcome gets a rendered sentence, [SyncSkipped] included
// (AC-3.2) — see [Sync.Reason].
func syncSection(run Run) string {
	sync := run.Sync
	if sync == nil || sync.Outcome == SyncUpToDate {
		return ""
	}

	var withBase string
	if sync.Base != "" {
		withBase = " with " + codeSpan(sync.Base)
	}

	var lead string
	switch sync.Outcome {
	case SyncSynced:
		lead = fmt.Sprintf("litefactory synced this branch%s before the run started: "+
			"it was %d commit(s) behind and is now %d ahead.", withBase, sync.Behind, sync.Ahead)
		if sync.CommitSHA != "" {
			lead += " Merge commit: " + codeSpan(sync.CommitSHA) + "."
		}

	case SyncBlocked:
		lead = fmt.Sprintf("litefactory could not sync this branch%s: merging would conflict, "+
			"so the branch was left as it was (%d commit(s) behind) and the run went ahead against it.",
			withBase, sync.Behind)

	case SyncSkipped:
		reason := oneLine(sync.Reason)
		if reason == "" {
			reason = "no reason was recorded for it"
		}
		lead = fmt.Sprintf("litefactory skipped syncing this branch%s (%d commit(s) behind): %s.",
			withBase, sync.Behind, reason)

	case SyncDeferredDirty:
		lead = fmt.Sprintf("litefactory deferred syncing this branch%s to preserve uncommitted changes "+
			"for this run to inspect and finish (%d commit(s) behind, %d ahead). "+
			"The base was not merged; this run proceeded on the current task base. "+
			"Synchronization will be reconsidered on the next eligible clean run.", withBase, sync.Behind, sync.Ahead)

	case SyncFailed:
		reason := oneLine(sanitize(sync.Reason))
		if reason == "" {
			reason = "the attempt failed without recording why"
		}
		lead = fmt.Sprintf("litefactory tried to sync this branch%s but could not: %s.", withBase, reason)

	default:
		lead = fmt.Sprintf("litefactory recorded a base branch sync outcome it does not recognise (%s).", sync.Outcome)
	}

	var b strings.Builder
	b.WriteString(syncHeading)
	b.WriteString(sectionGap)
	b.WriteString(lead)

	if sync.Outcome == SyncBlocked && len(sync.Conflicts) > 0 {
		b.WriteString(sectionGap)
		fmt.Fprintf(&b, "Conflicting path(s) (%d):\n", len(sync.Conflicts))
		b.WriteString(conflictsBlock(sync.Conflicts))
	}
	return b.String()
}

// conflictsBlock quotes conflicting paths inside a fenced block, so a path
// holding markdown characters — or one crafted to look like a code fence of
// its own — cannot become markup in the comment (S-2). It is the same
// defence [filesSection] uses for the files-touched list, applied here for
// the same reason: both lists are attacker-influenced content read from a
// worktree, and neither is trusted to render as anything but literal text.
//
// Before any of that, every path is classified by [classifyConflictPaths]
// (US-5): a path that could be read as an HTML comment shaped like
// [MarkerPrefix], or one carrying a bidi-override rune, is rejected
// outright — never printed, never printed truncated — and only counted
// (AC-5.1). What survives that check is then capped at
// [maxConflictPathEntries] with a remainder count; that cap is a display
// rule distinct from the rejection check and is applied after it, never in
// place of it (AC-5.2).
func conflictsBlock(paths []string) string {
	accepted, rejected := classifyConflictPaths(paths)

	shown := accepted
	var overflow int
	if len(shown) > maxConflictPathEntries {
		overflow = len(shown) - maxConflictPathEntries
		shown = shown[:maxConflictPathEntries]
	}

	var b strings.Builder
	if len(shown) > 0 {
		lines := make([]string, 0, len(shown))
		for _, p := range shown {
			lines = append(lines, sanitize(p))
		}
		list := strings.Join(lines, "\n")
		fence := fenceFor(list)
		b.WriteString(fence)
		b.WriteString("\n")
		b.WriteString(list)
		b.WriteString("\n")
		b.WriteString(fence)
	}
	if overflow > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "_… and %d more._", overflow)
	}
	if rejected > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, conflictPathRejectionSentence, rejected)
	}
	return b.String()
}

const resultHeading = "**Result**"

// resultOverlongNotice ends a final message that was over [maxResultBytes].
const resultOverlongNotice = "\n\n_[litefactory: the run's final message was longer than a comment can hold and was cut here.]_"

// resultSection renders the run's final message. It is rendered as markdown
// rather than quoted, because it is a message written to be read and its
// formatting is part of it; it is sanitised first, and the size cap keeps it
// from being the reason a body will not fit.
//
// Rendering it as markdown is what makes [closeOpenFence] necessary. The message
// is the one part of the body that is untrusted markup and is not wrapped in a
// fence of this package's own, so an unbalanced fence in it would run on and
// swallow everything below — including the log tail and the closing </details>.
// It is closed twice: once for a fence the model left open, and once for one the
// size cap cut open, with the notice outside the block where it can be read.
func resultSection(run Run, view outcomeView) string {
	res := run.Result
	if res == nil {
		return ""
	}
	text := closeOpenFence(strings.TrimSpace(sanitize(res.ResultText)))
	if text == "" {
		if !view.succeed {
			// The callout above already says what happened; an empty result on
			// a failure is expected rather than notable.
			return ""
		}
		return resultHeading + sectionGap + "_The run finished without a closing message._"
	}
	// Cut with no notice, close whatever the cut opened, then add the notice —
	// so the line explaining the cut is never itself inside a code block. The
	// room reserved covers the notice and the longest fence it could need.
	if cut, ok := truncateEnd(text, "", maxResultBytes-len(resultOverlongNotice)-maxFenceBytes); ok {
		text = closeOpenFence(cut) + resultOverlongNotice
	}
	return resultHeading + sectionGap + text
}

const filesHeading = "**Files touched**"

// filesSection renders the git-derived files-touched list (AC-4.4).
//
// It is quoted rather than bulleted so that a path containing markdown
// characters — or a newline, which git permits and -z preserves — cannot become
// markup in the comment.
func filesSection(run Run) string {
	if len(run.Files) == 0 {
		return filesHeading + sectionGap + "_No files were changed._"
	}

	shown := run.Files
	var overflow int
	if len(shown) > maxFileEntries {
		overflow = len(shown) - maxFileEntries
		shown = shown[:maxFileEntries]
	}

	lines := make([]string, 0, len(shown))
	for _, f := range shown {
		lines = append(lines, sanitize(f.String()))
	}
	list := strings.Join(lines, "\n")
	fence := fenceFor(list)

	var b strings.Builder
	fmt.Fprintf(&b, "**Files touched (%d)**%s", len(run.Files), sectionGap)
	b.WriteString(fence)
	b.WriteString("\n")
	b.WriteString(list)
	b.WriteString("\n")
	b.WriteString(fence)
	if overflow > 0 {
		fmt.Fprintf(&b, "%s_… and %d more, listed in full in the local log directory._", sectionGap, overflow)
	}
	return b.String()
}

// outputSummary is the <summary> line of the log tail block.
func outputSummary(truncated bool) string {
	s := fmt.Sprintf("Run output — last %d lines", TailLines)
	if truncated {
		s += ", with the middle removed to fit the comment size limit"
	}
	return s
}

// outputSection renders the collapsible log tail (AC-4.3, S-9). The caller
// passes the fence so that the space this block needs can be computed before
// the content is cut to fit it.
func outputSection(content, fence string, truncated bool) string {
	if strings.TrimSpace(content) == "" {
		return "**Run output**" + sectionGap + "_No output was captured for this run._"
	}
	var b strings.Builder
	b.WriteString("<details>\n<summary>")
	b.WriteString(outputSummary(truncated))
	b.WriteString("</summary>\n\n")
	b.WriteString(fence)
	b.WriteString("text\n")
	b.WriteString(strings.TrimRight(content, "\n"))
	b.WriteString("\n")
	b.WriteString(fence)
	b.WriteString("\n\n</details>")
	return b.String()
}

// outputOverhead is how many bytes [outputSection] adds around its content.
func outputOverhead(fence string, truncated bool) int {
	return len(outputSection("x", fence, truncated)) - 1
}

// elisionNotice builds the line that replaces the removed middle of the log
// tail. It names the byte and line count and the local log, so a shortened
// comment can never be mistaken for a complete one (AC-4.8).
func elisionNotice(logPath string) func(lines, bytes int) string {
	return func(lines, bytes int) string {
		where := "the local log"
		if logPath != "" {
			where = logPath
		}
		return fmt.Sprintf(
			"\n[litefactory: %d lines / %d bytes of output removed here to fit the comment size limit. The full output is at %s]\n\n",
			lines, bytes, where)
	}
}

// droppedOutputNotice replaces the log tail entirely when there is no room for
// a useful amount of it.
func droppedOutputNotice(logPath string) string {
	return "**Run output**" + sectionGap +
		"_Omitted: this report filled the comment size limit before the output could be quoted. The full, unredacted output is at " +
		logLocation(logPath) + "._"
}

// resultCutNotice ends a final message that had to be shortened.
func resultCutNotice(logPath string) string {
	return sectionGap + "_[litefactory: cut to fit the comment size limit. The full run is at " + logLocation(logPath) + ".]_"
}

// droppedResultNotice replaces a final message there was no room for at all.
func droppedResultNotice(logPath string) string {
	return resultHeading + sectionGap +
		"_Omitted to fit the comment size limit. The run's final message is in the local log at " +
		logLocation(logPath) + "._"
}

// logLocation renders the local log path for a notice, or a phrase standing in
// for it when the run never recorded one.
func logLocation(logPath string) string {
	if logPath == "" {
		return "the local log"
	}
	return codeSpan(logPath)
}

// codeSpan quotes a value inline, choosing a delimiter no backtick inside it
// can close.
func codeSpan(s string) string {
	s = oneLine(s)
	if s == "" {
		return "``"
	}
	longest := longestRun(s, '`')
	if longest == 0 {
		return "`" + s + "`"
	}
	d := strings.Repeat("`", longest+1)
	return d + " " + s + " " + d
}

// longestRun returns the length of the longest consecutive run of c in s.
func longestRun(s string, c byte) int {
	longest, run := 0, 0
	for i := range len(s) {
		if s[i] != c {
			run = 0
			continue
		}
		run++
		if run > longest {
			longest = run
		}
	}
	return longest
}

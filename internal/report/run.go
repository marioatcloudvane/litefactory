package report

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
)

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
	// Ref is the task this run belongs to. It appears in the marker, so it must
	// validate.
	Ref naming.Ref

	// Ordinal is the run's number within its session — the N in run=N and the
	// NNNN in the log filename. Must be 1 or more.
	Ordinal int

	// Result is what the harness recorded. Nil means litefactory never got a
	// result back, which is itself reportable: the run is rendered with an
	// unknown outcome rather than silently as a success.
	Result *harness.RunResult

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
			lead:  fmt.Sprintf("litefactory could not get run %d as far as `claude`: %s", run.Ordinal, failure),
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

	if run.WorktreePath != "" {
		v.lead += "\n\nWhatever the run produced is still in the worktree at " + codeSpan(run.WorktreePath) + "."
	}
	return v
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

	switch res.Outcome {
	case harness.OutcomeSucceeded:
		return outcomeView{word: "succeeded", succeed: true}

	case harness.OutcomeFailed:
		lead := fmt.Sprintf("Run %d failed: `claude` exited with status %d.", n, res.ExitCode)
		if res.ExitCode == harness.SignalExitCode {
			lead = fmt.Sprintf("Run %d failed without an exit status of its own.", n)
		}
		return outcomeView{word: "failed", alert: "CAUTION", lead: lead}

	case harness.OutcomeTimedOut:
		lead := fmt.Sprintf("Run %d ran past `claude.timeout_seconds` and litefactory killed it", n)
		if d, ok := run.duration(); ok {
			lead += " after " + formatDuration(d)
		}
		lead += ". The work it had done up to that point is still in the worktree. " +
			"Raise the timeout or narrow the task, then comment again."
		return outcomeView{word: "timed out", alert: "CAUTION", lead: lead}

	case harness.OutcomeAbandoned:
		lead := fmt.Sprintf("Run %d was abandoned because litefactory was shutting down. "+
			"Nothing failed and nothing crashed: the daemon was asked to stop while the run was in flight, "+
			"so it stopped. Comment again to run it.", n)
		return outcomeView{word: "abandoned", alert: "NOTE", lead: lead}

	case harness.OutcomeNoResult:
		lead := fmt.Sprintf("Run %d exited cleanly without ever reporting a result. "+
			"In practice this is usually a stale `claude` credential: the CLI retries internally about ten times "+
			"and then goes quiet. Run `claude` once by hand to check it is still logged in, then comment again.", n)
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
	b.WriteString(sectionGap)
	fmt.Fprintf(&b, "### litefactory · run %d · %s", run.Ordinal, view.word)
	if view.alert != "" {
		b.WriteString(sectionGap)
		b.WriteString(alertBlock(view.alert, view.lead))
	}
	b.WriteString(sectionGap)
	b.WriteString(metadataList(run))
	b.WriteString(sectionGap)
	b.WriteString(ReplyPlainlyNotice)
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
func metadataList(run Run) string {
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
	case run.Branch != "":
		add("Commit", "none — this run committed nothing")
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
		case harness.SignalExitCode:
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
		if res.HasResult && res.CostUSD > 0 {
			add("Cost", fmt.Sprintf("$%.4f", res.CostUSD))
		}
		if res.Model != "" {
			add("Model", codeSpan(res.Model))
		}
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

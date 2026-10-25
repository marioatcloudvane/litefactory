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

const (
	// maxResultBytes caps the run's final message. A model can emit a message
	// far larger than a comment, and the summary is the part that must never be
	// the reason a body is over the limit, so it is bounded on the way in
	// rather than trimmed as an emergency later.
	maxResultBytes = 32 << 10

	// maxFenceBytes is the room [resultSection] reserves for a closing fence
	// [closeOpenFence] may have to append after a cut. A run of more than 64
	// backticks in a final message is not worth sizing anything for, and
	// [runBody]'s budget pass is the limit that actually has to hold.
	maxFenceBytes = 64

	// minTailBytes is the least amount of room worth giving the run's final
	// message when [runBody] has to cut it to fit. Below this the message is
	// dropped whole and replaced with a line naming the local log, because a
	// dozen surviving bytes of it is not evidence of anything.
	minTailBytes = 512
)

// StoryTally is how many of a managed feature's stories were done as of the
// moment a run finished, and how many the feature has in total. It exists
// only to drive [Run.Stories]'s "partial" callout (S-2): this package never
// computes it and never looks past these two integers — no story key, title,
// issue number or parent ref ever enters a rendered body.
type StoryTally struct {
	// Done is how many of the feature's stories count as finished.
	Done int
	// Total is how many stories the feature has.
	Total int
}

// valid reports whether t is worth rendering: present, with a positive total
// and a done count that is neither negative nor over it. A nil or invalid
// tally is treated identically — omitted rather than guessed at (AC-017) —
// which is why every caller of this reaches it through a method that already
// tolerates a nil receiver rather than a nil check of its own.
func (t *StoryTally) valid() bool {
	return t != nil && t.Total > 0 && t.Done >= 0 && t.Done <= t.Total
}

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
	PreparationCaveats       []string
	ConflictProposal         *ConflictProposal
	ConflictSuggestionFailed bool

	// Meter contains prepared frozen metering values, independent of legacy
	// cost. It is retained as an input even though the run comment no longer
	// renders it.
	Meter                           *MeterSummary
	Recovery                        *Recovery
	SourceStoryIssue                int
	SourceCommentID                 int64
	SourceParentURL, SourceReplyURL string
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

	// Publication distinguishes existing/preparation commits from agent edits.
	Publication string

	// Files is the files-touched list from the worktree's status: a plain
	// git-derived list of the run's changes plus its untracked additions. It is
	// retained as an input even though the run comment no longer renders it.
	Files []git.FileChange

	// Output is the run's output as captured locally: stream text, stderr, or
	// both, and the unredacted whole of it stays in $LF_HOME/logs. It is
	// retained as an input even though the run comment no longer quotes it.
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
	// from — empty when no profile was selected. Both are retained as inputs
	// even though the run comment no longer renders them.
	//
	// ToolProfile is a validated profile name from the operator's own config. No
	// text originating from the forge is ever carried here.
	ToolsRequested []string
	ToolProfile    string

	// Sync and TaskSync retain the separate preparation observations. Only
	// unresolved freshness contributes to the combined preparation caveat;
	// current, absent and successfully synchronized branches stay quiet.
	Sync     *Sync
	TaskSync *Sync

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

	// Stories is the story-completion tally for this run's managed feature
	// (S-2). Nil means no tally applies — a caller that predates this field, a
	// run from any mode other than Implement, a feature that is not managed,
	// or one where the store read needed to compute it failed — and the report
	// renders exactly as it did before this field existed. A non-nil tally
	// that is not [StoryTally.valid] renders the same way: this package omits
	// an unusable count rather than guess at it (AC-017).
	Stories *StoryTally
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
	result bool
}

func (c cuts) any() bool {
	return c.result
}

// Section indices into the assembled body. The order is the reading order and
// also the survival order: everything above the run's final message outranks
// it, so even [redact.Redactor.Apply]'s own last-resort cut from the bottom
// takes the expendable part first.
const (
	secFixed  = iota // marker, heading, status callout, metadata — never cut
	secSync          // base branch sync disclosure — never cut (US-3)
	secResult        // the run's final message
	numSections
)

// sectionGap separates two sections of the body.
const sectionGap = "\n\n"

// runBody assembles the report and trims it to the budget, reporting what it
// had to remove.
//
// The trimming order cuts the run's final message first, because it is the one
// section a run can make arbitrarily long — the caps already on it make going
// further close to unreachable. Only if that still leaves the body over
// budget does the failure diagnostic in the fixed section itself get shrunk.
func (r *Reporter) runBody(run Run) (string, cuts) {
	var c cuts
	view := describeOutcome(run)
	logPath := run.logPath()
	inspection := RunInspectionCommand(run.Ref, run.Ordinal) + " --raw"
	if logPath == "" {
		inspection = TaskInspectionCommand(run.Ref.String())
	}

	var sections [numSections]string
	sections[secFixed] = fixedSection(run, view)
	sections[secSync] = r.preparationSection(run, len(sections[secFixed]))
	sections[secResult] = resultSection(run, view)

	if body := join(sections[:]); len(body) <= r.budget {
		return body, c
	}

	// 1. The run's final message, cut from the end: its opening is the part
	//    that says what happened.
	if sections[secResult] != "" {
		if room := r.roomFor(sections, secResult); room > len(resultHeading)+minTailBytes {
			sections[secResult], c.result = truncateEnd(sections[secResult], resultCutNotice(logPath, inspection), room)
		} else {
			sections[secResult] = droppedResultNotice(logPath, inspection)
			c.result = true
		}
	}
	if body := join(sections[:]); len(body) <= r.budget {
		return body, c
	}

	// 2. Last resort: reaching here means the fixed section alone is near the
	//    budget, which takes a pathological log path or task ref. Shrink the
	//    failure diagnostic — the only other variable-length text left — and
	//    retry; each retry removes at least the overflow plus literal markup
	//    room, and guidance too short to be useful is omitted altogether.
	if run.Result != nil && run.Result.Outcome == agent.OutcomeFailed && run.Result.Diagnostic != "" {
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
//
// The partial substitution (S-2) applies only on the success arm, after the
// failure fold below: a non-success outcome always wins over partial
// (AC-014), and that ordering is what makes it hold structurally rather than
// by a separate check. [describeRunOutcome] itself is never touched by it.
func describeOutcome(run Run) outcomeView {
	view := describeRunOutcome(run)
	if run.Failure != nil {
		return view.withFailure(run)
	}
	if view.succeed && run.Stories.valid() && run.Stories.Done < run.Stories.Total {
		return partialOutcomeView(run.Stories)
	}
	return view
}

// partialOutcomeView is the callout for a run that succeeded but did not
// finish every story in its managed feature (S-2, AC-011). It is a NOTE
// rather than a CAUTION or WARNING — nothing failed — and succeed stays true,
// which is what lets a later non-success check (AC-014) and the commit-line
// logic in [metadataList] keep treating this run as the success it was. The
// lead sentence names only the two integers in tally: never a story key,
// title, issue number or parent ref, and never the words "fail", "failed" or
// "error" (AC-011).
func partialOutcomeView(tally *StoryTally) outcomeView {
	return outcomeView{
		word:  "partial",
		alert: "NOTE",
		lead: fmt.Sprintf("This run finished %d of %d stories in the managed feature. The rest are still open.",
			tally.Done, tally.Total),
		succeed: true,
	}
}

// withFailure folds a failure that was not the run's own into the callout.
//
// The three cases are different reports, not one report with a footnote. A turn
// that never reached a coding agent has no run to describe; a run that worked but
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
			lead:  fmt.Sprintf("Run %d did not run: %s", run.Ordinal, failure),
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

	// Only mention the worktree here when there is a commit: with
	// CommitSHA != "", metadataList renders no Commit line at all, so this is
	// the single place that names the path. With CommitSHA == "",
	// metadataList's own Commit line already names the path for a non-success
	// run with nothing committed — naming it here too would say it twice.
	if run.CommitSHA != "" {
		v.lead += worktreeNote(run.WorktreePath)
	}
	if run.Result == nil || v.word == "not completed" {
		v.lead += "\n\nInspect on the factory host: `" + RunInspectionCommand(run.Ref, run.Ordinal) + "`."
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

// finalResultRejectedMarker is how turn finalization begins the diagnostic it
// records when it rejects a run's shared final result after the agent exits.
const finalResultRejectedMarker = "Shared final result rejected"

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
			lead:  fmt.Sprintf("litefactory did not record how run %d ended. Completion and the cause are unknown. Inspect on the factory host: `%s`.", n, RunInspectionCommand(run.Ref, n)),
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
		// A clean exit whose final result the factory then rejected is not a
		// process failure; leading with "exited with status 0" hides the real
		// cause, which the diagnostic below names (#431).
		if res.ExitCode == 0 && strings.Contains(res.Diagnostic, finalResultRejectedMarker) {
			lead = fmt.Sprintf("Run %d failed: `%s` exited cleanly, but litefactory rejected its final result after exit. "+
				"Nothing was committed; the diagnostic below names the cause.", n, tool)
		}
		// Guidance is a neutral adapter value, not arbitrary Reason content.
		// Quote it literally and cap it before it enters the fixed report budget.
		if diagnostic := oneLine(res.Diagnostic); diagnostic != "" {
			diagnostic, _ = truncateEnd(diagnostic, "…", 1024)
			lead += "\n\nDiagnostic: " + codeSpan(diagnostic)
		}
		lead += "\n\nInspect on the factory host: `" + RunInspectionCommand(run.Ref, n) + "`."
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
		lead := fmt.Sprintf("Run %d is recorded as abandoned. "+
			"Completion and any external effects are not confirmed. "+
			"Inspect the retained work on the factory host before deciding whether to continue: `%s`.", n, RunInspectionCommand(run.Ref, n))
		return outcomeView{word: "abandoned", alert: "NOTE", lead: lead}

	case agent.OutcomeNoResult:
		lead := fmt.Sprintf("Run %d exited without a recorded result. Completion and the cause are unknown. Inspect on the factory host: `%s`.", n, RunInspectionCommand(run.Ref, n))
		return outcomeView{word: "no result", alert: "WARNING", lead: lead}

	default:
		return outcomeView{
			word:  "outcome unrecognised",
			alert: "CAUTION",
			lead: fmt.Sprintf("Run %d finished with an outcome litefactory does not recognise (%s). "+
				"Completion is unknown. Inspect on the factory host: `%s`.", n, res.Outcome, RunInspectionCommand(run.Ref, n)),
		}
	}
}

// fixedSection renders the part of the body that is never cut: the marker,
// the heading, the status callout, whatever [metadataList] has left to say and
// the session-limit recovery section, when there is one. It is called "fixed"
// for the same reason it always was: everything here sits in secFixed, the
// section [runBody] never trims, so what a reader needs most — where
// uncommitted work still is, or how to reach the research brief — survives
// every level of truncation (AC-4.5, AC-4.8).
//
// metadataList may have nothing left to say at all — a succeeded run with
// nothing committed and no brief to link renders none of its bullets — and
// this function skips the gap around an empty list rather than leave a blank
// paragraph in its place.
func fixedSection(run Run, view outcomeView) string {
	var b strings.Builder
	b.WriteString(StatusMarker(run.Ref, run.Ordinal))
	b.WriteString(storySource(run.Ref, run.SourceStoryIssue, run.SourceCommentID, run.SourceParentURL, run.SourceReplyURL))
	b.WriteString(sectionGap)
	fmt.Fprintf(&b, "### litefactory · run %d · %s", run.Ordinal, view.word)
	if view.alert != "" {
		b.WriteString(sectionGap)
		b.WriteString(alertBlock(view.alert, view.lead))
	}
	if list := metadataList(run, view); list != "" {
		b.WriteString(sectionGap)
		b.WriteString(list)
	}
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

// metadataList renders what survives of the run's facts as a bullet list:
// where uncommitted work still is when a non-success run left nothing
// committed, whether an existing commit was left unpublished, and a link to
// the task's research brief. Every entry is omitted rather than guessed when
// it is not known, and the list itself may be empty — a succeeded run that
// committed nothing new and has no brief to link renders none of these.
//
// view is the outcome already computed for the callout above — passed in
// rather than recomputed, so the Commit line's wording and the callout's
// never disagree about whether this run succeeded.
func metadataList(run Run, view outcomeView) string {
	var lines []string
	add := func(label, value string) {
		lines = append(lines, "- **"+label+":** "+value)
	}

	// The one Commit case that survives (AC-3.1): a non-success run that
	// committed nothing still needs to say where whatever it did produce still
	// is. A run with no branch has no worktree to name, and a run that either
	// committed something or succeeded says nothing about its commit here at
	// all — this comment no longer names a commit's SHA or push state.
	if run.CommitSHA == "" && run.Branch != "" && !view.succeed {
		add("Commit", "none — this run did not succeed; if it produced anything, it is still in the worktree"+
			inWorktreeAt(run.WorktreePath))
	}

	// Publication only renders for the one state worth disclosing here: a
	// commit exists but never reached the remote. Every success-shaped
	// publication string — a push that went fine, or nothing committed at all
	// — is dropped along with it.
	if run.CommitSHA != "" && !run.Pushed && run.Publication != "" {
		add("Publication", sanitize(run.Publication))
	}

	// The brief sits in secFixed, the one section [runBody] never trims — a
	// link that only survives on short comments is a link nobody can rely on.
	if link, ok := run.briefLink(); ok {
		add("Research brief", link)
	}

	return strings.Join(lines, "\n")
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
	Phase               string
	LocalSHA, RemoteSHA string
	// Outcome is the synchronization decision reached.
	Outcome SyncOutcome

	// Behind and Ahead are the divergence from Base that was measured before
	// Outcome was decided: how many commits Base had that the branch did
	// not, and how many the branch had that Base did not.
	Behind, Ahead int
	// CountsUnavailable is true when a graph retained a condition without
	// measuring divergence. Do not invent counts for that observation.
	CountsUnavailable bool

	// Base is the base ref sync compared against and, when Outcome is
	// [SyncSynced], the ref that was merged in — for example
	// "origin/main". Rendered as plain text.
	Base string

	// CommitSHA is the merge commit sync produced, non-empty only when
	// Outcome is [SyncSynced] and the merge actually moved the branch.
	CommitSHA string

	// Conflicts retains detailed observed paths for callers. The concise
	// preparation passage does not render path lists.
	Conflicts []string

	// Reason is why sync did not complete, in a sentence fit to render
	// directly after "because ": why it was skipped (the configured cap,
	// auto-sync being off, or an `Sync: Off` label) when Outcome is
	// [SyncSkipped], or what went wrong when Outcome is [SyncFailed]. Empty
	// still renders a sentence rather than nothing — a skip is never silent
	// (AC-3.2) even when the caller did not record why.
	Reason string
}

// PreparationCaveat renders one bounded passage shared by ordinary and graph
// results. Resolved freshness never creates a warning.
func PreparationCaveat(caveats []string, syncs ...*Sync) string {
	reasons := make([]string, 0, len(caveats)+len(syncs))
	add := func(reason string) {
		reason = oneLine(sanitize(reason))
		if reason != "" && !slices.Contains(reasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	for _, c := range caveats {
		add(c)
	}
	for _, s := range syncs {
		if s == nil || s.Outcome == SyncUpToDate || s.Outcome == SyncSynced || string(s.Outcome) == "absent" {
			continue
		}
		if s.Outcome == SyncSkipped && s.Behind == 0 {
			continue
		}
		phase := s.Phase
		if phase == "" {
			phase = "base"
		}
		reason := s.Reason
		if reason == "" {
			switch s.Outcome {
			case SyncBlocked:
				reason = "merging would conflict; existing work was preserved"
			case SyncDeferredDirty:
				reason = "uncommitted changes were preserved"
			default:
				reason = "synchronization remains unresolved"
			}
		}
		if s.CountsUnavailable {
			add(fmt.Sprintf("%s synchronization: %s.", phase, reason))
		} else {
			add(fmt.Sprintf("%s synchronization (%d behind): %s.", phase, s.Behind, reason))
		}
	}
	if len(reasons) == 0 {
		return ""
	}
	return "**Preparation caveat**" + sectionGap + strings.Join(reasons, " ")
}

// preparationSection keeps advisory proposals in the original run's normal
// delivery. Prepared diffs are expendable; mandatory confirmation metadata is
// retained before them and no proposal is applied by rendering it.
func (r *Reporter) preparationSection(run Run, fixedBytes int) string {
	caveats := append([]string(nil), run.PreparationCaveats...)
	if run.ConflictSuggestionFailed {
		caveats = append(caveats, conflictSuggestionFailedSentence)
	}
	section := PreparationCaveat(caveats, run.TaskSync, run.Sync)
	if run.ConflictProposal == nil {
		return section
	}
	metadata, err := conflictPreviewMetadata(Reply{Ref: run.Ref, ConflictProposal: run.ConflictProposal})
	if err != nil {
		return section
	}
	metadata = strings.TrimPrefix(metadata, ReplyMarker(run.Ref)+sectionGap)
	if section == "" {
		section = "**Preparation caveat**"
	}
	section += sectionGap + metadata
	// Leave room for the fixed run status and an omitted-result notice.
	limit := r.budget - fixedBytes - 1024
	if len(section)+len(conflictDiffOmission) > limit {
		// Never truncate complete proposal metadata into misleading confirmation
		// advice. The durable proposal remains available for inspection.
		return PreparationCaveat(append(caveats, "Suggested conflict resolution could not fit this result; resolve manually or regenerate."), run.TaskSync, run.Sync)
	}
	omitted := false
	for _, file := range run.ConflictProposal.Files {
		content := sanitize(file.Diff)
		if content == "" {
			continue
		}
		fence := fenceFor(content)
		block := sectionGap + fence + "\n" + strconv.Quote(sanitize(file.Path)) + "\n" + content + "\n" + fence
		if len(section)+len(block)+len(conflictDiffOmission) > limit {
			omitted = true
			continue
		}
		section += block
	}
	if omitted {
		section += conflictDiffOmission
	}
	return section
}

// conflictsBlock quotes conflicting paths inside a fenced block, so a path
// holding markdown characters — or one crafted to look like a code fence of
// its own — cannot become markup in the comment (S-2). The list is
// attacker-influenced content read from a worktree, and it is not trusted to
// render as anything but literal text.
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
// swallow whatever this package renders below it. It is closed twice: once for
// a fence the model left open, and once for one the size cap cut open, with
// the notice outside the block where it can be read.
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
	notice := resultOverlongNotice + completeRunInspection(run)
	if cut, ok := truncateEnd(text, "", maxResultBytes-len(notice)-maxFenceBytes); ok {
		text = closeOpenFence(cut) + notice
	}
	return resultHeading + sectionGap + text
}

// completeRunInspection uses the unfiltered raw route so omitted commands and
// consequences in the saved final event remain available to the operator.
func completeRunInspection(run Run) string {
	if run.logPath() == "" {
		return "\n\nNo saved event log is identified. Ask the factory operator to inspect retained diagnostics on the factory host: `" + TaskInspectionCommand(run.Ref.String()) + "`."
	}
	return "\n\nAsk the factory operator to inspect the saved event log on the factory host: `" + RunInspectionCommand(run.Ref, run.Ordinal) + " --raw`."
}

// resultCutNotice ends a final message that had to be shortened.
func resultCutNotice(logPath, inspection string) string {
	if logPath == "" {
		return sectionGap + "_[litefactory: cut to fit the comment size limit; no saved event log is identified.]_" + sectionGap + "Ask the factory operator to inspect retained diagnostics on the factory host: `" + inspection + "`."
	}
	return sectionGap + "_[litefactory: cut to fit the comment size limit. The full run is at " + logLocation(logPath) + ".]_" + sectionGap + "Ask the factory operator to inspect the saved event log on the factory host: `" + inspection + "`."
}

// droppedResultNotice replaces a final message there was no room for at all.
func droppedResultNotice(logPath, inspection string) string {
	if logPath == "" {
		return resultHeading + sectionGap + "_Omitted to fit the comment size limit; no saved event log is identified._" + sectionGap + "Ask the factory operator to inspect retained diagnostics on the factory host: `" + inspection + "`."
	}
	return resultHeading + sectionGap +
		"_Omitted to fit the comment size limit. The run's final message is in the local log at " +
		logLocation(logPath) + "._" + sectionGap + "Ask the factory operator to inspect the saved event log on the factory host: `" + inspection + "`."
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

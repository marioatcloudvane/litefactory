package turn

import (
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Disposition is what a turn did. It is the first thing a caller reads, and it
// is what says whether there is anything to report.
//
// It exists because "did the turn fail" is three different questions —
// was it even attempted, did it get as far as running claude, and did that run
// succeed — and collapsing them into a boolean is how a lock contention gets
// posted to an issue as a failure.
type Disposition int

const (
	// DispositionRan means a runs row was allocated and its outcome recorded.
	// [Result.Outcome] says how the run ended, including the case where claude
	// never started: the row exists, so the run has to be reported (AC-4.1).
	DispositionRan Disposition = iota

	// DispositionLocked means another daemon instance holds this task's lock.
	// Nothing was prepared and nothing ran. The triggering comment must be left
	// unconsumed so a later tick picks it up again — there is no queue and no
	// "busy" reply (AC-3.1).
	DispositionLocked

	// DispositionAlreadyReported means a runs row for this triggering comment
	// exists and has already been reported. The turn is a no-op: crash replay
	// must never run the same comment twice (AC-3.4).
	DispositionAlreadyReported

	// DispositionAlreadyRan means a runs row for this triggering comment exists
	// but has not been reported yet — a daemon that died between the run and
	// the comment, whose orphan reconciliation then failed the row (D-B).
	// [Result.Run] carries it so the report can be posted without re-running
	// claude.
	DispositionAlreadyRan

	// DispositionPrepareFailed means the turn could not reach the point of
	// starting claude: the fetch failed (AC-3.5), the worktree path is occupied
	// (AC-3.9), the slug post-condition did not hold (AC-3.7). No run row was
	// allocated, so nothing was consumed from the run ordinal sequence — and
	// [Result.Reason] still has to be posted, because silence is not an outcome.
	DispositionPrepareFailed

	// DispositionAgentSwitchRefused means the turn's resolved agent does not
	// own this session's segment (US-3): a prior run started this
	// conversation under one coding-agent tool, and this run's labels
	// resolved to a different one. Nothing was created and nothing was
	// mutated — [Result.AgentOwning] and [Result.AgentRequested] carry the
	// two closed-set names the reply names, and [Reporter.ReportTurn]
	// answers with [report.ReplyAgentSwitchRefused] rather than a run report.
	DispositionAgentSwitchRefused

	// DispositionSyncRefused means the turn's task branch could not be merged
	// with the base branch cleanly (US-4): [git.Worktree.MergePreview] found
	// conflicting paths and the configured policy
	// ([Options.BlockRunOnConflict]) says a conflict of this kind stops the
	// run rather than merely disclosing it. Nothing was created and nothing
	// was mutated — [Result.SyncConflicts] carries the paths that collided —
	// and [Reporter.ReportTurn] answers with [report.ReplyRefusesSync] rather
	// than a run report.
	DispositionSyncRefused
)

// String renders the disposition for logs and test failures. The values are
// stable identifiers rather than prose.
func (d Disposition) String() string {
	switch d {
	case DispositionRan:
		return "ran"
	case DispositionLocked:
		return "locked"
	case DispositionAlreadyReported:
		return "already_reported"
	case DispositionAlreadyRan:
		return "already_ran"
	case DispositionPrepareFailed:
		return "prepare_failed"
	case DispositionAgentSwitchRefused:
		return "agent_switch_refused"
	case DispositionSyncRefused:
		return "sync_refused"
	default:
		return "invalid"
	}
}

// Result is everything one turn produced, and the only thing internal/report
// needs in order to write the comment.
//
// It is a plain struct, deliberately: the reporting path consumes it directly,
// and every field here is a fact about this turn rather than a rendering
// decision. Fields that a given disposition cannot have — a commit sha for a
// turn that never ran — are left zero, and the documented dispositions say
// which those are.
type Result struct {
	// Disposition is what the turn did. Read it first: Outcome, Run and the
	// commit fields are only meaningful for the dispositions documented to set
	// them.
	Disposition Disposition

	// SessionID and TriggerCommentID identify the turn, and are set on every
	// path including [DispositionLocked].
	SessionID        int64
	TriggerCommentID int64

	// Ref is the canonical task, github:owner/name#123. Zero only when the
	// session row could not be read at all.
	Ref naming.Ref

	// Session is the sessions row as it stands after the turn: the frozen slug,
	// the branch and the worktree path are all on it.
	Session store.Session

	// Run is the runs row. It is the authoritative record of the run — status,
	// exit code, duration, cost, log path, ordinal — and is the zero value when
	// no run was allocated ([DispositionLocked], [DispositionPrepareFailed]).
	Run store.Run

	// Harness is what the subprocess produced: the result text, the stderr
	// tail, the event counts. It is nil when no subprocess ran. It carries the
	// material S-9 and AC-4.3 want quoted in the comment; the durable facts are
	// on Run.
	Harness *harness.RunResult

	// Outcome is how the run ended, and is meaningful only when
	// [Result.Reportable] is true. A preparation failure records
	// [harness.OutcomeFailed]; the five harness outcomes are otherwise passed
	// through unchanged, because a timeout, an abandonment and a plain non-zero
	// exit are three different things (AC-3.20).
	Outcome harness.Outcome

	// Reason explains a non-success outcome in one line, empty on success. It
	// is what the failure comment leads with (S-9).
	Reason string

	// Branch is the task branch, and WorktreePath is where the work is on this
	// machine. WorktreePath is quoted in the report when a push failed, because
	// it is where the commit still is (AC-3.24).
	Branch       string
	WorktreePath string

	// SHABefore and SHAAfter bracket the run (AC-3.24). SHAAfter is empty when
	// the worktree was never inspected after the run.
	SHABefore string
	SHAAfter  string

	// CommitSHA is the commit this turn made, empty when it made none.
	CommitSHA string

	// Changed reports that the branch moved: this turn committed, or the run
	// committed for itself. False is the AC-3.23 path — no changes, no commit,
	// no push — and the report has to say so explicitly rather than implying an
	// empty success.
	Changed bool

	// Committed reports that this turn made the commit, as opposed to the run
	// having made its own.
	Committed bool

	// Pushed reports that the branch reached the remote. False with a non-nil
	// [Result.Err] is the push-failure path: the commit is intact in
	// WorktreePath.
	Pushed bool

	// Remote is the remote the branch was pushed to, "origin" at M0.
	Remote string

	// Changes are the files the run touched, as git reported them before they
	// were staged: the plain git-derived list AC-4.4 asks for.
	Changes []git.FileChange

	// MetaPath and LogPath are the run's two artefacts under $LF_HOME. Both are
	// absolute, and LogPath is the path the failure comment quotes (AC-4.5).
	MetaPath string
	LogPath  string

	// ToolsRequested is the tool surface this turn asked claude for, and
	// ToolProfile is the canonical mode profile it came from. Both are set from
	// the request, so they survive a
	// turn that never started a subprocess; the resolved surface, which only an
	// init event can supply, stays on Harness.
	//
	// ToolProfile is selected from the resolved mode, never forge text.
	ToolsRequested []string
	ToolProfile    string

	// Err is the failure that was not claude's: a fetch that did not run, a
	// worktree that could not be created, a commit or push that did not
	// complete. A *git.PushError here names the worktree still holding the
	// commit. It is nil on the happy path and on a run that merely failed.
	Err error

	// AgentOwning and AgentRequested are set only on
	// [DispositionAgentSwitchRefused]: the coding-agent tool this session's
	// segment already belongs to, and the one this turn's labels resolved to.
	// Both have already passed the agent-name shape check the label ladder
	// applies (internal/daemon's agentKindName), which is what makes them
	// safe for [Reporter.ReportTurn] to quote into a comment — internal/report
	// checks them against its own pattern again before it does (S-8).
	AgentOwning    string
	AgentRequested string

	// SyncConflicts is set only on [DispositionSyncRefused]: the paths
	// [git.Worktree.MergePreview] reported as conflicting, exactly as
	// syncBase recorded them. internal/report renders them inside a fenced
	// block (S-2) and does not sanitise or decide which paths are safe to
	// show at all — that is US-5's job.
	SyncConflicts []string
}

// Reportable reports whether this turn owes the issue a comment. Every
// disposition but a contended lock and an already-reported replay does
// (AC-4.1).
func (r Result) Reportable() bool {
	switch r.Disposition {
	case DispositionRan, DispositionAlreadyRan, DispositionPrepareFailed, DispositionAgentSwitchRefused,
		DispositionSyncRefused:
		return true
	case DispositionLocked, DispositionAlreadyReported:
		return false
	default:
		return false
	}
}

// ConsumesComment reports whether the triggering comment may be marked as
// handled once this turn has been reported.
//
// A contended lock is the one case that must not consume it: the comment has to
// be seen again on a later tick, which is what makes "no queue" safe (AC-3.1).
// An already-reported replay does not consume it either, because the watermark
// moved past it in the same transaction that reported it.
func (r Result) ConsumesComment() bool {
	switch r.Disposition {
	case DispositionRan, DispositionAlreadyRan, DispositionPrepareFailed, DispositionAgentSwitchRefused,
		DispositionSyncRefused:
		return true
	case DispositionLocked, DispositionAlreadyReported:
		return false
	default:
		return false
	}
}

// Succeeded reports whether the turn is a clean success: claude finished
// cleanly and whatever it produced is safe on the remote.
//
// A run that succeeded but whose push failed is not a success. The work is not
// lost — it is committed in [Result.WorktreePath] — but a report that called it
// a success would be lying about where the code is (AC-3.24).
func (r Result) Succeeded() bool {
	return r.Disposition == DispositionRan &&
		r.Outcome == harness.OutcomeSucceeded &&
		r.Err == nil
}

// Files returns the paths the run touched, in the order git reported them. It
// is the files-touched list of the report (AC-4.4).
func (r Result) Files() []string {
	if len(r.Changes) == 0 {
		return nil
	}
	paths := make([]string, 0, len(r.Changes))
	for _, change := range r.Changes {
		paths = append(paths, change.Path)
	}
	return paths
}

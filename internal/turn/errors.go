package turn

import (
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/store"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
)

// The conditions [Execute] reports through its error return.
//
// Everything else that can go wrong during a turn — a fetch that failed, a
// worktree path someone else is using, a claude that exited non-zero — is not
// an error here. It is a [Result], because it has to be reported to the issue
// (AC-4.1) and a value is how that gets carried out of this package.
var (
	// ErrInvalidOptions reports dependencies a turn cannot run with: a nil
	// database, a relative $LF_HOME, an empty daemon instance id.
	ErrInvalidOptions = errors.New("invalid turn options")

	// ErrInvalidRequest reports a request that is not a turn: no session, no
	// triggering comment, or a command that does not spawn a run.
	ErrInvalidRequest = errors.New("invalid turn request")

	// ErrBugCommentRefused reports an owner comment the Bug workflow could
	// not apply. The refusal was already posted and the comment consumed, so
	// the caller logs it and moves on to the task's next comment.
	ErrBugCommentRefused = errors.New("bug comment refused and consumed")
)

func invalidOptions(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidOptions, fmt.Sprintf(format, args...))
}

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

// failure is a turn that could not be carried out because of the state of the
// world rather than a defect in litefactory: the remote was unreachable, the
// worktree path is occupied, the clone has no committer identity.
//
// It is unexported on purpose. A caller never has to recognise the type: a
// failure surfaces as [DispositionPrepareFailed] with [Result.Reason] set to
// this one-line explanation and [Result.Err] carrying the cause, which is
// everything the report needs and nothing it has to switch on.
//
// The distinction it draws is the one that decides whether a comment gets
// posted. A failure is reported to the issue and the comment is consumed; an
// ordinary error travels back to the daemon, which logs it and leaves the
// comment for the next tick.
type failure struct {
	reason string
	err    error
}

func (f *failure) Error() string {
	if f.err == nil {
		return f.reason
	}
	return f.reason + ": " + f.err.Error()
}

func (f *failure) Unwrap() error { return f.err }

// failuref wraps err as a reportable failure with a one-line reason.
func failuref(err error, format string, args ...any) *failure {
	return &failure{reason: fmt.Sprintf(format, args...), err: err}
}

// agentSwitchRefusal is an automatic-recovery continuation whose named
// segment does not own the coding-agent tool this turn's labels resolved to
// (US-3): the segment already belongs to owning, and this turn's labels
// resolved to requested instead. An ordinary turn no longer reaches this —
// prepareDeliverySegment reconciles an ordinary turn's segment to the newly
// resolved tool in place instead of refusing — so this now only fires when a
// label changed between when the continuation was recorded and when it was
// dispatched, and the segment it names can no longer be continued under the
// tool it was recorded for.
//
// It is unexported and distinct from [failure] on purpose. A [failure]
// becomes a run-failure comment through [prepareFailed]; this becomes
// [DispositionAgentSwitchRefused] through [agentSwitchRefused] and is
// answered with [report.ReplyAgentSwitchRefused] — a permission answer named
// by two closed-set values, not a stack trace. [Execute] tells the two apart
// with [errors.As] before it ever reaches the generic *failure branch.
type agentSwitchRefusal struct {
	owning    string
	requested string
}

func (e *agentSwitchRefusal) Error() string {
	return fmt.Sprintf("this task's conversation belongs to the %q coding-agent tool, not %q",
		e.owning, e.requested)
}

// syncRefusal is a turn whose task branch could not be merged with the base
// branch cleanly (US-4): [git.Worktree.MergePreview] found conflicting paths,
// and [Options.BlockRunOnConflict] says a conflict of this kind stops the run
// rather than merely disclosing it.
//
// It is unexported and distinct from [failure] on purpose, the same reason
// [agentSwitchRefusal] is: a [failure] becomes a run-failure comment through
// [prepareFailed]; this becomes [DispositionSyncRefused] through
// [syncRefused] and is answered with [report.ReplyRefusesSync] — a named
// refusal that lists the conflicting paths, not a stack trace. [Execute]
// tells the two apart with [errors.As] before it ever reaches the generic
// *failure branch.
type syncRefusal struct {
	phase       string
	observation *syncResult
	conflicts   []string

	// tooFarBehind marks the second reason a sync refuses, added after
	// US-4: the branch is further behind than [Options.AutoSyncMaxBehind]
	// allows, so no merge was previewed at all and conflicts is nil.
	// behind and maxBehind carry the two numbers the reply names.
	//
	// This case is a refusal rather than a skip on purpose. Skipping let
	// the run proceed against a branch known to be stale — the exact
	// failure [Options.AutoSyncBase] exists to prevent — while saying so
	// only in a report nobody had to read. A cap means "too far to merge
	// unattended", and the honest answer is to stop.
	tooFarBehind      bool
	behind, maxBehind int

	// branch, base, files and excluded are US-11's addition: the conflicting
	// content [syncBase] collected and capped inside the same
	// [Options.CloneGate] acquisition [git.Worktree.MergePreview] ran under,
	// carried through so [attemptConflictSuggestion] can ask a
	// suggested-resolution advisor for it after the gate has been released.
	// files and excluded are both nil unless [Options.SuggestConflictResolution]
	// was true for this turn (S-10).
	branch, base     string
	collectionReason string
	snapshots        []git.ConflictSnapshot
	files            []agent.ConflictFileContent
	excluded         []string

	// baseSHA and branchSHA are US-12's addition: the base and branch tips
	// [git.Worktree.MergePreview] was evaluated against, read while still
	// inside the same [Options.CloneGate] acquisition (S-15). They are what
	// a later confirmation's re-verification compares its own fresh reads
	// against — always populated when Outcome is blocked, independently of
	// [Options.SuggestConflictResolution], the same way branch and base
	// themselves are.
	baseSHA, branchSHA string
	inputs             store.ConflictProposalInputs
}

func (e *syncRefusal) Error() string {
	if e.observation != nil {
		return taskRefusalDetail(e.observation)
	}
	if e.tooFarBehind {
		return fmt.Sprintf("this task's branch is %d commit(s) behind the base branch, more than the cap of %d",
			e.behind, e.maxBehind)
	}
	return fmt.Sprintf("the base branch could not be merged into this task's branch cleanly: %d conflicting file(s)",
		len(e.conflicts))
}

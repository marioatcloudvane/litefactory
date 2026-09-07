package turn

import (
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The two conditions [Execute] reports through its error return.
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

// agentSwitchRefusal is a turn whose resolved coding-agent tool does not own
// this session's segment (US-3): the segment already belongs to owning, and
// this turn's labels resolved to requested instead.
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
	conflicts []string

	// branch, base, files and excluded are US-11's addition: the conflicting
	// content [syncBase] collected and capped inside the same
	// [Options.CloneGate] acquisition [git.Worktree.MergePreview] ran under,
	// carried through so [attemptConflictSuggestion] can ask a
	// suggested-resolution advisor for it after the gate has been released.
	// files and excluded are both nil unless [Options.SuggestConflictResolution]
	// was true for this turn (S-10).
	branch, base string
	files        []agent.ConflictFileContent
	excluded     []string
}

func (e *syncRefusal) Error() string {
	return fmt.Sprintf("the base branch could not be merged into this task's branch cleanly: %d conflicting file(s)",
		len(e.conflicts))
}

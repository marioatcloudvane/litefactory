package turn

import (
	"errors"
	"fmt"
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

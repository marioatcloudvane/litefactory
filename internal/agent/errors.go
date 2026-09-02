package agent

import (
	"errors"
	"fmt"
)

// Sentinel conditions shared by every [Runner] implementation.
var (
	// ErrInvalidRequest reports a [Request] that cannot be run: a missing
	// worktree, an empty prompt, an ordinal below one.
	ErrInvalidRequest = errors.New("invalid run request")

	// ErrSessionNotResumable reports that a [Runner] declined to continue
	// the conversation a request asked to resume: [SessionRef] names no
	// conversation the tool can find. It is the one run failure a
	// [Runner] reports as a Go error as well as a [*RunResult], because the
	// caller's answer to it is to rerun the same request cold rather than
	// to report it — see [Runner].
	//
	// It MUST be decided from the shape of the run — exit code, whether a
	// model or tool surface was ever resolved — never from the wording of
	// the tool's own stderr, which belongs to another project's binary and
	// is not a contract. The run result carrying the stderr tail is
	// returned alongside it as evidence.
	ErrSessionNotResumable = errors.New("the coding-agent tool has no conversation to resume")
)

// invalidRequest builds an [ErrInvalidRequest] with a field-level
// explanation.
func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

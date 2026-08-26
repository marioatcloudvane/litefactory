package harness

import (
	"errors"
	"fmt"
)

// Sentinel conditions. They separate the two kinds of failure this package can
// report: a request or configuration that is wrong, which is a programming or
// setup error, and a run that went badly, which is not an error at all — see
// [Runner.Run] for why a failed run returns a [*RunResult] with a nil error.
var (
	// ErrInvalidRequest reports a [Request] that cannot be run: a missing
	// worktree, an empty prompt, an ordinal below one.
	ErrInvalidRequest = errors.New("invalid run request")

	// ErrInvalidOptions reports [Options] that cannot produce a usable
	// [Runner]: a relative $LF_HOME, an empty tool surface, a timeout of zero.
	ErrInvalidOptions = errors.New("invalid harness options")

	// ErrStartFailed reports that the claude process could not be started at
	// all, so no run happened and there is nothing to report on.
	ErrStartFailed = errors.New("claude did not start")

	// ErrSessionNotResumable reports that claude declined to continue the
	// conversation a run asked to resume: the session id names no transcript it
	// can find. It is the one run failure this package reports as an error as
	// well as a [*RunResult], because the caller's answer to it is to rerun the
	// same run cold rather than to report it — see [Runner.Run].
	//
	// It is decided from the shape of the run, never from the wording of
	// claude's stderr; the run result carrying the stderr tail is returned
	// alongside it as evidence.
	ErrSessionNotResumable = errors.New("claude has no conversation to resume")

	// ErrInvalidPrompt reports a [PromptInput] that cannot produce a usable
	// prompt: no instruction, no feature slug, an unparseable task ref. It is
	// separate from [ErrInvalidRequest] because prompt assembly is a pure
	// function a caller may use — and test — without going near a subprocess.
	ErrInvalidPrompt = errors.New("invalid prompt input")
)

// invalidRequest builds an [ErrInvalidRequest] with a field-level explanation.
func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

// invalidOptions builds an [ErrInvalidOptions] with a field-level explanation.
func invalidOptions(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidOptions, fmt.Sprintf(format, args...))
}

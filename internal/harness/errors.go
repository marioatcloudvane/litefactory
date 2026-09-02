package harness

import (
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Sentinel conditions. They separate the two kinds of failure this package can
// report: a request or configuration that is wrong, which is a programming or
// setup error, and a run that went badly, which is not an error at all — see
// [Runner.Run] for why a failed run returns a [*RunResult] with a nil error.
var (
	// ErrInvalidRequest is [agent.ErrInvalidRequest], aliased here for the
	// same reason [ErrSessionNotResumable] is: [agent.Request.Validate]'s
	// generic checks and this package's own Claude-specific ones
	// ([Runner.validateRequest] — a missing worktree, an empty prompt, an
	// ordinal below one) both have to unwrap to one sentinel, or a caller
	// checking errors.Is against either would get the wrong answer depending
	// on which half of validation rejected the request.
	ErrInvalidRequest = agent.ErrInvalidRequest

	// ErrInvalidOptions reports [Options] that cannot produce a usable
	// [Runner]: a relative $LF_HOME, an empty tool surface, a timeout of zero.
	ErrInvalidOptions = errors.New("invalid harness options")

	// ErrStartFailed reports that the claude process could not be started at
	// all, so no run happened and there is nothing to report on.
	ErrStartFailed = errors.New("claude did not start")

	// ErrSessionNotResumable is [agent.ErrSessionNotResumable], aliased here
	// so call sites written before this package became the Claude
	// implementation of [agent.Runner] keep compiling. It reports that
	// claude declined to continue the conversation a run asked to resume:
	// the session id names no transcript it can find. It is the one run
	// failure this package reports as an error as well as a [*RunResult],
	// because the caller's answer to it is to rerun the same run cold
	// rather than to report it — see [Runner.Run].
	//
	// It is decided from the shape of the run, never from the wording of
	// claude's stderr; the run result carrying the stderr tail is returned
	// alongside it as evidence.
	ErrSessionNotResumable = agent.ErrSessionNotResumable

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

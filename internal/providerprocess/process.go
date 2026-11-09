// Package providerprocess validates factory-scoped execution requests and owns
// durable conversation coordination. Native process mechanics live in the
// execution library; this package does not grant provider capabilities.
package providerprocess

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

// ErrProviderExit marks a completed native process that returned a nonzero exit status.
// It does not imply availability; adapters must examine native terminal diagnostics.
var ErrProviderExit = execution.ErrProviderExit

// OversizedObserver receives, once per judge run and after the process ends,
// the judge transport's report of output it skipped instead of ending the run:
// kind is "line_dropped" when at least one line over the line budget was
// discarded, otherwise "total_exceeded" when the run's stdout passed the
// non-judge total budget. Sizes are in bytes.
type OversizedObserver = execution.OversizedObserver

// RunJudgeProcess validates the isolated advisory catalog before bounded I/O.
// An advisory judge does not need oversized output: a line over the line
// budget is discarded rather than ending the run, and the total budget does
// not apply; the timeout remains the bound. A non-nil oversized observer is
// told once, after the process ends, when output was skipped or passed the
// old total budget.
func RunJudgeProcess(ctx context.Context, binary string, args, env []string, request agent.JudgeRequest, timeout time.Duration, observe func([]byte), oversized OversizedObserver) error {
	return RunJudgeProcessObserved(ctx, binary, args, env, request, timeout, observe, nil, oversized)
}

// RunJudgeProcessObserved is [RunJudgeProcess] with a failure observer.
func RunJudgeProcessObserved(ctx context.Context, binary string, args, env []string, request agent.JudgeRequest, timeout time.Duration, observe func([]byte), failure FailureObserver, oversized OversizedObserver) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return execution.RunProcess(ctx, binary, args, env, request.Invocation.Prompt, request.Invocation.WorkingDirectory, timeout, observe, failure, nil, true, oversized)
}

// FailureObserver receives a nonzero exit status and the bounded tail of the
// provider's standard error. Stderr never crosses this package boundary in a
// returned error; an observer that keeps it must redact it first and must not
// persist it. A nil observer discards stderr, as before.
type FailureObserver = execution.FailureObserver

// RunChatProcess shares bounded provider I/O for a response-only request. The
// provider adapter owns the verified gate and explicit no-tools launch flags.
func RunChatProcess(ctx context.Context, binary string, args, env []string, request agent.ChatRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return execution.RunProcess(ctx, binary, args, env, request.Prompt, request.WorkingDirectory, timeout, observe, nil, nil, false, nil)
}

// RunDriftProcess is bounded transport; the provider owns restricted admission.
func RunDriftProcess(ctx context.Context, binary string, args, env []string, request agent.DriftRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return execution.RunProcess(ctx, binary, args, env, request.Invocation.Prompt, request.Invocation.WorkingDirectory, timeout, observe, nil, nil, false, nil)
}

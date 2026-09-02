package harness

import "github.com/cloudvane/litefactory/internal/agent"

// Outcome, RunResult and the run-path vocabulary are internal/agent's now —
// this package is the Claude implementation of [agent.Runner], and these
// aliases exist so that the many call sites written against harness.Outcome,
// harness.RunResult and harness.PathResume before this package became that
// implementation keep compiling unchanged. New code SHOULD prefer the
// internal/agent names directly; both name exactly the same type or value.
type (
	Outcome     = agent.Outcome
	RunResult   = agent.RunResult
	RunPath     = agent.RunPath
	Request     = agent.Request
	Permissions = agent.Permissions
	SessionRef  = agent.SessionRef
)

const (
	OutcomeSucceeded = agent.OutcomeSucceeded
	OutcomeFailed    = agent.OutcomeFailed
	OutcomeTimedOut  = agent.OutcomeTimedOut
	OutcomeAbandoned = agent.OutcomeAbandoned
	OutcomeNoResult  = agent.OutcomeNoResult

	// SignalExitCode is the exit code recorded when there is no exit status
	// to record: the process was killed by the timeout or the shutdown, so
	// the number on the runs row is -1 and the reason string carries the
	// meaning (S-9).
	SignalExitCode = agent.SignalExitCode

	PathCold                   = agent.PathCold
	PathResume                 = agent.PathResume
	PathColdAfterResumeRefused = agent.PathColdAfterResumeRefused
)

// ToolSurface is the shape a resolved tool profile narrows
// [agent.Permissions.Payload] into for a Claude run: the --tools list and
// the --allowedTools list beside it (SD-2).
//
// It is the one place internal/turn is allowed to know what a Claude
// permission grant looks like, and it exists precisely so internal/agent
// does not have to: nothing outside this package and internal/turn's request
// assembly ever narrows a Payload into this type.
type ToolSurface struct {
	// Tools is the --tools surface this run is granted. Empty means no
	// profile applied and the runner falls back to its configured surface;
	// it is never a request to grant no tools at all, because an omitted
	// --tools is allow-all.
	Tools []string

	// AllowedTools is the --allowedTools permission list belonging with
	// Tools. It is additive to the operator's ambient settings and
	// therefore defence in depth, not the boundary: nothing is granted by a
	// rule here that Tools does not already allow.
	AllowedTools []string
}

// toolSurface narrows req's opaque permissions payload into this package's
// own type, or the zero value when no profile applied to this request.
func toolSurface(req agent.Request) ToolSurface {
	ts, _ := req.Permissions.Payload.(ToolSurface)
	return ts
}

package harness

import "github.com/cloudvane/litefactory/internal/agent"

// Outcome is the vendor-neutral result outcome.
type Outcome = agent.Outcome

// RunResult is a vendor-neutral run result.
type RunResult = agent.RunResult

// RunPath is a vendor-neutral conversation path.
type RunPath = agent.RunPath

// Request is a vendor-neutral run request.
type Request = agent.Request

// Permissions is a vendor-specific permission payload.
type Permissions = agent.Permissions

// SessionRef is a vendor-neutral conversation identity.
type SessionRef = agent.SessionRef

const ( //nolint:revive // Compatibility aliases intentionally retain established exported names.
	// OutcomeSucceeded and the following constants alias agent outcomes.
	OutcomeSucceeded = agent.OutcomeSucceeded
	OutcomeFailed    = agent.OutcomeFailed //nolint:revive // Compatibility alias.
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

// ToolSurface is the shape a mode-selected profile narrows
// [agent.Permissions.Payload] into for a Claude run: its tool list and its
// sandbox.
//
// It is the one place internal/turn is allowed to know what a Claude
// permission grant looks like, and it exists precisely so internal/agent
// does not have to: nothing outside this package and internal/turn's request
// assembly ever narrows a Payload into this type.
type ToolSurface struct {
	// Tools is the --tools surface of this run. Empty means
	// [config.AgentTools]; it is never a request to grant no tools at all,
	// because an omitted --tools is allow-all.
	Tools []string

	// ReadOnly selects the read-only sandbox: the run's Bash commands run in
	// an OS sandbox in which the worktree is unwritable and the network is
	// closed. The mode's profile chooses it.
	ReadOnly bool
}

// toolSurface narrows req's opaque permissions payload into this package's
// own type, or the zero value when no mode profile was supplied to this request.
func toolSurface(req agent.Request) ToolSurface {
	ts, _ := req.Permissions.Payload.(ToolSurface)
	return ts
}

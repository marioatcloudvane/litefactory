package agent

import "context"

// Kind identifies which coding-agent-tool implementation a [Runner] drives.
//
// It carries no vocabulary of its own — there is no fixed set of values here
// to switch on. A Kind is valid exactly when a [Runner] is registered for it
// in the internal/turn.RunnerSet the daemon's factory built at startup; a
// value with no such registration is refused there, not here. That is what
// lets a third implementation be additive: it does not require a new case in
// this package or in internal/report, because neither of them switches on
// Kind at all.
type Kind string

// String renders the kind for logs and metadata.
func (k Kind) String() string { return string(k) }

// Runner runs one coding-agent-tool process for a single request and
// reports what it produced.
//
// internal/harness's Claude implementation and internal/codex's Codex one
// (once its argv and event shape are measured) both satisfy it, which is the
// whole point of the type: a caller holding a Runner does not know or care
// which tool is behind it, and adding a third tool means writing a new
// implementation rather than touching every place that currently assumes
// one.
//
// A run that went badly is not an error: it comes back as a [*RunResult]
// whose Outcome says how it failed, because every run that actually happened
// has to be reported, logged and have its metadata written. A non-nil error
// return means the run never happened at all — an invalid request, a binary
// that could not be started — with [ErrSessionNotResumable] as the single
// documented exception: it comes back with a result and as an error, because
// the caller's answer to it is to rerun the same request cold rather than to
// report it.
type Runner interface {
	Run(ctx context.Context, req Request) (*RunResult, error)
}

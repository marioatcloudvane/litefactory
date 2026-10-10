package cli

import (
	"errors"
	"fmt"
)

// Process exit codes. These are part of the tool's contract with scripts and
// launchd; do not add new ones without updating the documented convention.
const (
	// ExitOK signals success.
	ExitOK = 0
	// ExitFailure signals a generic runtime failure.
	ExitFailure = 1
	// ExitUsage signals the command line itself was wrong: unknown command,
	// bad flag, wrong number of arguments.
	ExitUsage = 2
	// ExitPrecondition signals the command was well-formed but the machine is
	// not in a state where it can run (missing config, bot == owner, and so on).
	ExitPrecondition = 3
)

// UsageError marks an error as caused by the command line rather than by the
// work the command was asked to do. It exits with [ExitUsage] and prints the
// usage text for cmd (or the top-level usage when cmd is nil).
type UsageError struct {
	// Cmd is the command whose usage should be printed. Nil means the
	// top-level usage.
	Cmd *Command
	// Usage, when non-empty, is printed verbatim instead of the usage text
	// implied by Cmd. It carries the narrower help — a command group's
	// subcommands, say — for cases where the whole tool's usage would be
	// noise.
	Usage string
	Err   error
}

func (e *UsageError) Error() string { return e.Err.Error() }

func (e *UsageError) Unwrap() error { return e.Err }

// Usagef builds a [UsageError] for cmd. Pass a nil cmd for errors that are not
// attributable to a single command.
func Usagef(cmd *Command, format string, args ...any) error {
	return &UsageError{Cmd: cmd, Err: fmt.Errorf(format, args...)}
}

// PreconditionError marks an error as an unmet precondition: the command is
// valid but the environment is not ready for it. It exits with
// [ExitPrecondition].
//
// Domain packages should not import this type. They return their own sentinel
// or typed errors, and the command wrapper in this package translates them —
// that keeps the dependency direction cli -> domain.
type PreconditionError struct {
	Err error
}

func (e *PreconditionError) Error() string { return e.Err.Error() }

func (e *PreconditionError) Unwrap() error { return e.Err }

// Precondition wraps err as a [PreconditionError].
func Precondition(err error) error { return &PreconditionError{Err: err} }

// Preconditionf builds a [PreconditionError] from a format string.
func Preconditionf(format string, args ...any) error {
	return &PreconditionError{Err: fmt.Errorf(format, args...)}
}

// ExitCode maps an error returned by a [Handler] to a process exit code.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.As(err, new(*UsageError)):
		return ExitUsage
	case errors.As(err, new(*PreconditionError)):
		return ExitPrecondition
	default:
		return ExitFailure
	}
}

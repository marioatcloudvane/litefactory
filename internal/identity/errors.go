package identity

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel conditions, so a caller can tell which check failed by name rather
// than by matching on gh's prose. Every one of them means the same thing to
// `init` — exit 3 — but they mean very different things to the person reading
// the message.
var (
	// ErrNotInstalled reports that gh could not be found on PATH. The error
	// wrapping it names the PATH that was searched, because under launchd that
	// PATH is /usr/bin:/bin:/usr/sbin:/sbin and nothing else.
	ErrNotInstalled = errors.New("the GitHub CLI (gh) is not installed or is not on PATH")

	// ErrNotAuthenticated reports a gh that is installed but not logged in.
	ErrNotAuthenticated = errors.New("gh is not logged in; run `gh auth login`")

	// ErrUnsupportedHost reports a gh logged in somewhere other than
	// github.com (AC-0.8).
	ErrUnsupportedHost = errors.New("only github.com is supported")

	// ErrNoIdentity reports a gh that answered without a login or without a
	// numeric id. The numeric id is load-bearing — it is what the self-filter
	// compares (D-I) — so a nameless or idless answer is a failure, not a
	// partial success.
	ErrNoIdentity = errors.New("gh reported no login or no numeric user id")
)

// CommandError reports a gh invocation that failed.
//
// The command line and stderr are fields rather than a pre-formatted message
// because AC-0.4 requires `init` to print precisely those two things, and a
// caller should not have to reverse-engineer them out of a string.
//
// It is safe to print. The only gh command here that touches a credential is
// `gh auth status`, which masks the token itself unless it is asked for with
// --show-token — and nothing in this package passes that flag.
type CommandError struct {
	// Binary is the gh executable that ran.
	Binary string
	// Args are the arguments passed to gh, without the binary itself.
	Args []string
	// ExitCode is gh's exit status, or -1 when the process never ran or was
	// killed by a signal.
	ExitCode int
	// Stderr is everything gh wrote to its standard error, trailing
	// whitespace removed.
	Stderr string
	// TimedOut reports that the per-command deadline fired, as opposed to the
	// caller's context being cancelled or gh exiting non-zero.
	TimedOut bool
	// Timeout is the deadline that fired, meaningful only when TimedOut.
	Timeout time.Duration
	// Err is the underlying cause: an *exec.ExitError, a context error, or one
	// of the sentinels above once the failure has been classified.
	Err error
}

// Command returns the exact command line that was run.
func (e *CommandError) Command() string { return commandLine(e.Binary, e.Args...) }

func (e *CommandError) Error() string {
	var b strings.Builder
	b.WriteString(e.Command())
	if e.TimedOut {
		fmt.Fprintf(&b, ": timed out after %s", e.Timeout)
	} else if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	if e.Stderr != "" {
		b.WriteString(": ")
		b.WriteString(e.Stderr)
	}
	return b.String()
}

func (e *CommandError) Unwrap() error { return e.Err }

// commandLine renders a binary and its arguments as the command line a user
// could paste into a shell.
func commandLine(binary string, args ...string) string {
	if binary == "" {
		binary = binaryName
	}
	return strings.Join(append([]string{binary}, args...), " ")
}

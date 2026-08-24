package launchd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// This file is the whole subprocess surface of the package: launchctl is
// executed here and nowhere else, and every caller goes through [Runner].

// launchctlBinary is macOS's launchctl, referenced by absolute path so the
// lookup cannot be redirected by whatever PATH the process inherited.
const launchctlBinary = "/bin/launchctl"

// DefaultTimeout bounds one launchctl invocation. launchctl talks to a local
// daemon over IPC and either answers immediately or is not going to; the
// generous bound exists so that a wedged launchd surfaces as an error rather
// than as a `daemon start` that never returns.
const DefaultTimeout = 30 * time.Second

// maxStderr caps how much of launchctl's stderr is quoted back in an error.
const maxStderr = 400

// launchctl's exit statuses for "that service is not here". Both are reachable
// and they differ by subcommand: bootout answers 3 ("Boot-out failed: 3: No
// such process") and print answers 113 ("Could not find service ... in domain
// for user gui: 501").
const (
	exitNoSuchProcess = 3
	exitNoSuchService = 113
)

// ErrNotLoaded reports that the agent is not bootstrapped into the GUI domain.
// It is a normal state, not a failure: `daemon stop` on a stopped daemon
// succeeds, and `daemon status` reports "not loaded".
var ErrNotLoaded = errors.New("agent is not loaded")

// Runner executes one launchctl subcommand and returns its standard output.
//
// It is declared here, at the point of consumption, rather than in a shared
// abstraction package. There is exactly one production implementation ([Exec]);
// the interface exists so that install, uninstall and status are testable
// without bootstrapping a real LaunchAgent onto the machine running the tests.
type Runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// Exec is the production [Runner]. It runs the real launchctl.
type Exec struct {
	// Binary is the launchctl executable. Empty means /bin/launchctl.
	Binary string
	// Timeout bounds one invocation. Zero means [DefaultTimeout].
	Timeout time.Duration
}

// Run executes launchctl with args and returns its standard output, trailing
// newline removed. A non-zero exit yields a [*CommandError] carrying the exact
// command, its status and its stderr, so a caller can print them verbatim.
func (e Exec) Run(ctx context.Context, args ...string) (string, error) {
	binary := e.Binary
	if binary == "" {
		binary = launchctlBinary
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// #nosec G204 -- the binary is a fixed absolute path and the arguments are
	// service targets and a plist path assembled by this package, with no shell
	// between us and execve.
	cmd := exec.CommandContext(cmdCtx, binary, args...)
	cmd.Stdin = nil

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return strings.TrimRight(stdout.String(), "\n"), nil
	}

	cmdErr := &CommandError{
		Binary:   binary,
		Args:     slices.Clone(args),
		ExitCode: -1,
		Stderr:   diagnostics(stderr.String()),
		Err:      err,
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		cmdErr.ExitCode = exitErr.ExitCode()
	}
	switch {
	case ctx.Err() != nil:
		cmdErr.Err = ctx.Err()
	case errors.Is(cmdCtx.Err(), context.DeadlineExceeded):
		cmdErr.Err = context.DeadlineExceeded
	}
	return "", cmdErr
}

// CommandError reports a launchctl invocation that failed.
type CommandError struct {
	// Binary is the launchctl that ran.
	Binary string
	// Args is the argument list, without the binary.
	Args []string
	// ExitCode is the process exit status, or -1 when it did not exit
	// normally.
	ExitCode int
	// Stderr is the first line of the tool's diagnostics.
	Stderr string
	// Err is the underlying cause.
	Err error
}

func (e *CommandError) Error() string {
	cmd := e.Binary + " " + strings.Join(e.Args, " ")
	switch {
	case e.Stderr != "" && e.ExitCode >= 0:
		return fmt.Sprintf("%s: exited %d: %s", cmd, e.ExitCode, e.Stderr)
	case e.ExitCode >= 0:
		return fmt.Sprintf("%s: exited %d", cmd, e.ExitCode)
	case e.Stderr != "":
		return fmt.Sprintf("%s: %v: %s", cmd, e.Err, e.Stderr)
	default:
		return fmt.Sprintf("%s: %v", cmd, e.Err)
	}
}

func (e *CommandError) Unwrap() error { return e.Err }

// isNotLoaded reports whether err is launchctl's way of saying the service is
// not in the domain.
//
// The exit status is the primary signal; the text match is a fallback for a
// macOS release that renumbers them, because misreading "not loaded" as a real
// failure would make `daemon stop` fail on an already-stopped daemon.
func isNotLoaded(err error) bool {
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	switch cmdErr.ExitCode {
	case exitNoSuchProcess, exitNoSuchService:
		return true
	}
	text := strings.ToLower(cmdErr.Stderr)
	return strings.Contains(text, "no such process") || strings.Contains(text, "could not find service")
}

// diagnostics condenses launchctl's stderr onto one line, capped.
//
// It joins the lines rather than taking the first, because launchctl leads
// with a bare "Bad request." and puts the sentence that names the problem —
// "Could not find service ..." — on the line after it.
func diagnostics(s string) string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	joined := []rune(strings.Join(lines, "; "))
	if len(joined) > maxStderr {
		return string(joined[:maxStderr]) + "…"
	}
	return string(joined)
}

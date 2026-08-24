package identity

// This file is the whole exec surface of the package, and of litefactory: it is
// the only place in the tree that runs the gh binary, and only `litefactory
// init` reaches it. policy_test.go fails the build if that stops being true.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// binaryName is what we look for on PATH.
const binaryName = "gh"

// DefaultTimeout bounds one gh invocation.
//
// Neither command here is local: `gh auth status` validates the stored token
// against the API, and both may first have to read that token out of the macOS
// keychain — which can raise an allow-access dialog that nobody answers. The
// deadline is what turns that into a diagnosable failure instead of an init
// that never returns.
const DefaultTimeout = 30 * time.Second

// waitDelay bounds how long Wait may block after gh itself has exited. gh hands
// its output to children, and a surviving child holding the pipe open would
// otherwise keep Wait blocked past the command's deadline.
const waitDelay = 5 * time.Second

// runner executes one gh command. It is unexported on purpose: handing a caller
// outside this package the ability to run an arbitrary gh command would make
// the "gh is identity discovery only" rule unenforceable.
type runner struct {
	binary  string
	env     []string
	timeout time.Duration
}

// newRunner resolves gh and builds the child environment it will be run with.
func newRunner(timeout time.Duration) (runner, error) {
	binary, err := lookup()
	if err != nil {
		return runner{}, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return runner{binary: binary, env: childEnv(os.Environ()), timeout: timeout}, nil
}

// lookup finds gh on PATH and reports the PATH it searched when it cannot. The
// answer is almost always that the caller's PATH is not the shell's — under
// launchd it is four directories and nothing else.
func lookup() (string, error) {
	searched := os.Getenv("PATH")
	if searched == "" {
		searched = fallbackPATH
	}
	path, err := exec.LookPath(binaryName)
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		return "", &notInstalledError{PATH: searched, Err: err}
	}
	return path, nil
}

// notInstalledError reports a gh that could not be resolved, naming the search
// path. It unwraps to both [ErrNotInstalled] and the lookup failure.
type notInstalledError struct {
	PATH string
	Err  error
}

func (e *notInstalledError) Error() string {
	return ErrNotInstalled.Error() + " (searched PATH=" + e.PATH + "): " + e.Err.Error()
}

func (e *notInstalledError) Unwrap() []error { return []error{ErrNotInstalled, e.Err} }

// output is what one gh invocation wrote. The two streams are kept apart so a
// failure can report gh's diagnostics without the data it printed — and so that
// `gh auth status`, which has written its answer to stdout in some versions and
// to stderr in others, can be read from both.
type output struct {
	stdout string
	stderr string
}

// run executes gh with args and waits up to the runner's timeout. The output is
// returned even when the command failed, because `gh auth status` says why it
// failed on the stream this package parses.
func (r runner) run(ctx context.Context, args ...string) (output, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// #nosec G204 -- this is the single sanctioned gh exec point. The binary is
	// an absolute path from lookup, every argument is a constant in this
	// package, and there is no shell between us and execve.
	cmd := exec.CommandContext(cmdCtx, r.binary, args...)
	// Somewhere that is guaranteed to exist and guaranteed not to be inside a
	// repository, so gh cannot pick up a clone's remotes as context.
	cmd.Dir = string(filepath.Separator)
	cmd.Env = r.env
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := output{
		stdout: strings.TrimRight(stdout.String(), "\r\n"),
		stderr: strings.TrimRight(stderr.String(), "\r\n \t"),
	}
	if err == nil {
		return out, nil
	}

	cmdErr := &CommandError{
		Binary:   r.binary,
		Args:     slices.Clone(args),
		ExitCode: -1,
		Stderr:   out.stderr,
		Err:      err,
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		cmdErr.ExitCode = exitErr.ExitCode()
	}
	switch {
	case ctx.Err() != nil:
		// The caller's context went away; that is the real cause, whatever
		// signal the process happened to die from.
		cmdErr.Err = ctx.Err()
	case errors.Is(cmdCtx.Err(), context.DeadlineExceeded):
		cmdErr.TimedOut = true
		cmdErr.Timeout = r.timeout
		cmdErr.Err = context.DeadlineExceeded
	}
	return out, cmdErr
}

// commandLineFor renders what run would execute, for messages about a command
// that succeeded but answered with nonsense.
func (r runner) commandLineFor(args ...string) string { return commandLine(r.binary, args...) }

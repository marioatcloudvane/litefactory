package git

// This file is the whole exec surface of the package. It is the only file in
// litefactory that runs the git binary; everything else in internal/git goes
// through the unexported runner below, and no other package execs git at all.
// Keep it that way — the guarantees in doc.go are only as good as this
// chokepoint.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// binaryName is what we look for on PATH.
const binaryName = "git"

// Per-command timeouts. An untimed git call is the difference between an error
// and a hung tick: a passphrase-protected key or a black-holed host will
// otherwise block forever with no output.
const (
	// readTimeout covers the local, read-only plumbing commands. They touch
	// only the object store and the config, so seconds are generous.
	readTimeout = 15 * time.Second
	// fetchTimeout covers network reads.
	fetchTimeout = 60 * time.Second
	// pushTimeout covers network writes, which carry the payload and so get
	// twice as long. It lives here with the other deadlines rather than with
	// the push itself, so the whole timeout policy is readable in one place.
	//
	//nolint:unused // consumed by the mutating Worktree operations (T-LF3.1).
	pushTimeout = 120 * time.Second
	// worktreeTimeout covers "git worktree add/remove", which checks a whole
	// tree in or out and is bounded by disk rather than by the network.
	worktreeTimeout = 120 * time.Second
)

// waitDelay bounds how long Wait may block after the git process itself has
// exited. git hands its stdout and stderr to children such as ssh, and a
// surviving child holding the pipe open would otherwise keep Wait blocked past
// the command's deadline.
const waitDelay = 5 * time.Second

// sshCommand keeps ssh non-interactive. BatchMode refuses to ask for a
// passphrase instead of blocking on a prompt no one can answer, and
// accept-new trusts a host key the first time but still fails on a changed one.
const sshCommand = "ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new"

// fallbackPATH is launchd's own PATH. A launchd-started daemon inherits exactly
// this, so it is the right value to fall back to when PATH is missing entirely
// rather than leaving the child unable to find ssh.
const fallbackPATH = "/usr/bin:/bin:/usr/sbin:/sbin"

// Binary is a resolved, validated git executable.
//
// Resolution happens once at startup rather than at the first fetch, so a
// missing git is a loud error at boot instead of a mysterious failure three
// hours into a run.
type Binary struct {
	path    string
	version string
}

// Resolve finds git on PATH, checks that it actually runs, and records its
// version. The returned error names the PATH that was searched — under launchd
// that PATH is /usr/bin:/bin:/usr/sbin:/sbin and nothing else, which is the
// usual reason a binary that works in a shell is not found by the daemon.
func Resolve(ctx context.Context) (*Binary, error) {
	searched := os.Getenv("PATH")
	if searched == "" {
		searched = fallbackPATH
	}
	path, err := exec.LookPath(binaryName)
	if err != nil {
		return nil, &BinaryNotFoundError{Name: binaryName, PATH: searched, Err: err}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, &BinaryNotFoundError{Name: binaryName, PATH: searched, Err: err}
	}

	// Running it is the only honest way to tell an executable git from a
	// same-named directory, a broken symlink or a quarantined binary.
	probe := runner{binary: abs, dir: workingDirForProbe()}
	version, err := probe.run(ctx, readTimeout, "--version")
	if err != nil {
		return nil, &BinaryNotFoundError{Name: abs, PATH: searched, Err: err}
	}
	return &Binary{path: abs, version: version}, nil
}

// workingDirForProbe returns a directory that is guaranteed to exist and to be
// outside any repository, so "git --version" cannot pick up a repo's config.
func workingDirForProbe() string { return string(filepath.Separator) }

// Path returns the absolute path of the resolved git executable.
func (b *Binary) Path() string {
	if b == nil {
		return ""
	}
	return b.path
}

// Version returns the "git version ..." line reported at resolution time.
func (b *Binary) Version() string {
	if b == nil {
		return ""
	}
	return b.version
}

// runner executes one git command in one directory. It is unexported on
// purpose: handing a caller outside this package the ability to run an
// arbitrary git command would defeat the [Clone] allowlist entirely.
//
// The zero runner is inert. Every method on [Clone] and [Worktree] ultimately
// calls run, so a value of either type that was not built by this package fails
// with [ErrUninitialized] instead of running git in the process working
// directory.
type runner struct {
	binary string
	dir    string
}

// run executes git with args, waits up to timeout, and returns stdout with the
// trailing newline removed. stdout and stderr are captured separately so a
// failure can report the command's diagnostics without the data it printed.
func (r runner) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	if r.binary == "" || r.dir == "" {
		return "", ErrUninitialized
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// #nosec G204 -- this is the single sanctioned git exec point. The binary
	// is an absolute path validated by Resolve, the arguments are assembled by
	// this package from values screened by checkName, and there is no shell
	// between us and execve.
	cmd := exec.CommandContext(cmdCtx, r.binary, args...)
	cmd.Dir = r.dir
	cmd.Env = childEnv()
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return strings.TrimRight(stdout.String(), "\n"), nil
	}

	cmdErr := &CommandError{
		Binary:   r.binary,
		Dir:      r.dir,
		Args:     slices.Clone(args),
		ExitCode: -1,
		Stderr:   strings.TrimRight(stderr.String(), "\r\n \t"),
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
		cmdErr.Timeout = timeout
		cmdErr.Err = context.DeadlineExceeded
	}
	return "", cmdErr
}

// childEnv builds the git child environment from an explicit allowlist. It is
// never os.Environ(): the daemon's own environment may hold the bot token under
// token_env, and git has no business seeing it.
//
// The list is SD-5, verified end to end over real SSH from a launchd-started
// process:
//
//	PATH, HOME, SSH_AUTH_SOCK,
//	GIT_TERMINAL_PROMPT=0,
//	GIT_SSH_COMMAND="ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new"
//
// GIT_ASKPASS and SSH_ASKPASS must be absent rather than empty, which is what
// building the slice from scratch gives us for free — an empty GIT_ASKPASS is
// not the same as an unset one and would still be consulted.
//
// Caveat: setting HOME does not redirect ssh's known_hosts or config lookup.
// OpenSSH resolves the user's home through getpwuid, not $HOME, so this
// environment isolates git but not ssh's own state. If that isolation is ever
// needed, pass -o UserKnownHostsFile= explicitly in sshCommand.
func childEnv() []string {
	env := make([]string, 0, 5)

	path := os.Getenv("PATH")
	if path == "" {
		path = fallbackPATH
	}
	env = append(env, "PATH="+path)

	// An empty HOME is worse than an absent one: git would look for its config
	// under "/" rather than falling back to the passwd database.
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	// The agent socket is how pushes authenticate. It is deliberately not
	// forwarded to the claude child (SD-4), only to git.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		env = append(env, "SSH_AUTH_SOCK="+sock)
	}

	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND="+sshCommand,
	)
}

// checkName refuses a caller-supplied value that git would read as an option.
// Every such value reaches git as a positional argument, and "--upload-pack=..."
// wearing the costume of a remote name is the classic way to turn a repository
// URL into command execution.
func checkName(what, value string) error {
	switch {
	case value == "":
		return fmt.Errorf("%s: %w: must not be empty", what, ErrInvalidArgument)
	case strings.HasPrefix(value, "-"):
		return fmt.Errorf("%s %q: %w: must not start with a dash", what, value, ErrInvalidArgument)
	case strings.ContainsAny(value, "\x00\n\r"):
		return fmt.Errorf("%s %q: %w: must not contain control characters", what, value, ErrInvalidArgument)
	}
	return nil
}

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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	pushTimeout = 120 * time.Second
	// worktreeTimeout covers "git worktree add/remove", which checks a whole
	// tree in or out and is bounded by disk rather than by the network.
	worktreeTimeout = 120 * time.Second
	// commitTimeout covers "git add" and "git commit". Both are bounded by
	// disk — a run can touch a large tree — and both may hand control to a
	// repository hook, so they get the same budget as a worktree checkout
	// rather than the read budget.
	commitTimeout = 120 * time.Second
	// divergenceTimeout covers "git rev-list --left-right --count", which
	// walks history between two refs. It touches only the local object store
	// — the remote-tracking ref it compares against was already updated by a
	// prior fetch — but a large or shallow-mismatched history can still walk
	// for a while, so it gets more than the plain read budget without
	// reaching for the network budget it does not need.
	divergenceTimeout = 30 * time.Second
	// mergePreviewTimeout covers "git merge-tree --write-tree", which computes
	// a merge without touching the working tree or any ref. It is local like
	// divergenceTimeout, but it also writes the resulting tree (and, on
	// conflict, the individual blobs) into the object database, so it gets a
	// little more room than a pure read.
	mergePreviewTimeout = 30 * time.Second
	// mergeTimeout covers "git merge", which updates the working tree, the
	// index and a ref, and may run hooks — the same budget as commitTimeout
	// for the same reasons.
	mergeTimeout = 120 * time.Second
	// showBlobTimeout covers "git show <tree>:<path>", which reads one blob
	// out of the local object store — no different in kind from the other
	// local, read-only plumbing readTimeout already covers, but kept as its
	// own named budget because [Worktree.ConflictBlob] is called once per
	// conflicting file and a large file is still a single local read, not a
	// history walk.
	showBlobTimeout = 15 * time.Second
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
	path               string
	version            string
	mergeTreeSupported bool
}

// minMergeTreeVersion is the git ≥2.38 floor "git merge-tree --write-tree"
// needs — the flag does not exist on anything older, and running it there
// fails with an "unknown option" usage error rather than a merge result.
var minMergeTreeVersion = gitVersion{major: 2, minor: 38}

// gitVersion is the major.minor a resolved git binary reports, which is all
// this package ever needs to compare against a feature floor.
type gitVersion struct {
	major, minor int
}

// atLeast reports whether v is the same as or newer than minimum.
func (v gitVersion) atLeast(minimum gitVersion) bool {
	if v.major != minimum.major {
		return v.major > minimum.major
	}
	return v.minor >= minimum.minor
}

// parseGitVersion extracts the major.minor out of "git version X.Y[.Z] ..." —
// the first field looking like a dotted version number wins, so "(Apple
// Git-155)" and similar vendor suffixes are never mistaken for one. A string
// this cannot parse is reported as unparsed rather than guessed at, so a
// future git that changes its banner format fails the 2.38 check safe rather
// than being assumed current.
func parseGitVersion(raw string) (gitVersion, bool) {
	for _, field := range strings.Fields(raw) {
		parts := strings.SplitN(field, ".", 3)
		if len(parts) < 2 {
			continue
		}
		major, majErr := strconv.Atoi(parts[0])
		minor, minErr := strconv.Atoi(parts[1])
		if majErr == nil && minErr == nil {
			return gitVersion{major: major, minor: minor}, true
		}
	}
	return gitVersion{}, false
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
	// An unparseable version string is treated as unsupported (see
	// [parseGitVersion]), not as a resolution failure: git still works for
	// everything except merge-tree --write-tree, and that narrower refusal is
	// [Worktree.MergePreview]'s to report, not boot's.
	parsed, ok := parseGitVersion(version)
	return &Binary{path: abs, version: version, mergeTreeSupported: ok && parsed.atLeast(minMergeTreeVersion)}, nil
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
	// indexFile is set only by retained-repair capture, to a private temporary
	// index. Ambient GIT_INDEX_FILE is never inherited.
	indexFile string
	// mergeTreeSupported carries [Binary]'s own version check down to every
	// [Clone] and [Worktree] built from it, so [Worktree.MergePreview] can
	// refuse before it ever execs rather than after git rejects an option
	// that version does not have.
	mergeTreeSupported bool
}

// run executes git with args, waits up to timeout, and returns stdout with the
// trailing newline removed. stdout and stderr are captured separately so a
// failure can report the command's diagnostics without the data it printed.
func (r runner) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	out, err := r.runRaw(ctx, timeout, args...)
	return strings.TrimRight(out, "\n"), err
}

// runRaw preserves exact blob bytes, including all trailing newlines.
func (r runner) runRaw(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return r.runRawLimit(ctx, timeout, 0, args...)
}

func (r runner) runRawLimit(ctx context.Context, timeout time.Duration, limit int, args ...string) (string, error) {
	return r.runRawRecordLimit(ctx, timeout, limit, 0, args...)
}

// runRawRecordLimit stops a NUL-delimited observation after a fixed number of
// fields. The complete final field is retained before cancelling the child.
func (r runner) runRawRecordLimit(ctx context.Context, timeout time.Duration, limit, fields int, args ...string) (string, error) {
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
	if r.indexFile != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+r.indexFile)
	}
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay

	stdout, stderr := gitBoundedOutput{limit: limit, fieldsRemaining: fields, cancel: cancel}, gitBoundedOutput{limit: limit, cancel: cancel}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return "", fmt.Errorf("git observation exceeds %d byte budget", limit)
	}
	if stdout.recordsReached && ctx.Err() == nil {
		return stdout.String(), nil
	}
	if err == nil {
		return stdout.String(), nil
	}
	return "", classifyRunError(ctx, cmdCtx, timeout, r, args, stderr.String(), err)
}

// runAllowingExit behaves like run, except that an exit code present in
// allowed is not treated as a failure: its stdout is returned alongside the
// exit code that produced it. This exists for commands where a non-zero exit
// is a reported outcome rather than an error — "git merge-tree --write-tree"
// exits 1 for a conflicted merge and still writes the merge result the caller
// is expected to read from stdout.
//
// A timed-out command, or one cancelled by ctx, is never folded into allowed,
// whatever exit code the OS happened to report: those stay distinguishable
// failures rather than being read as a reported outcome.
func (r runner) runAllowingExit(ctx context.Context, timeout time.Duration, allowed []int, args ...string) (stdout string, exitCode int, err error) {
	if r.binary == "" || r.dir == "" {
		return "", -1, ErrUninitialized
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// #nosec G204 -- same sanctioned exec point as run; see the comment there.
	cmd := exec.CommandContext(cmdCtx, r.binary, args...)
	cmd.Dir = r.dir
	cmd.Env = childEnv()
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay

	var outBuf, stderr bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := strings.TrimRight(outBuf.String(), "\n")
	if runErr == nil {
		return out, 0, nil
	}

	cmdErr := classifyRunError(ctx, cmdCtx, timeout, r, args, stderr.String(), runErr)
	if !cmdErr.TimedOut && ctx.Err() == nil && slices.Contains(allowed, cmdErr.ExitCode) {
		return out, cmdErr.ExitCode, nil
	}
	return "", cmdErr.ExitCode, cmdErr
}

// classifyRunError builds the *CommandError for a failed invocation, telling
// apart three causes: the caller's own context being cancelled, the
// per-command timeout firing, or git simply exiting non-zero. A timeout is
// never mistaken for a plain exit — TimedOut is the one bit callers such as
// runAllowingExit must never fold into an "allowed" outcome.
func classifyRunError(ctx, cmdCtx context.Context, timeout time.Duration, r runner, args []string, stderr string, err error) *CommandError {
	cmdErr := &CommandError{
		Binary:   r.binary,
		Dir:      r.dir,
		Args:     slices.Clone(args),
		ExitCode: -1,
		Stderr:   strings.TrimRight(stderr, "\r\n \t"),
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
	return cmdErr
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

// gitBoundedOutput cancels excessive output and drains buffered pipe data, so a
// large observation cannot allocate or keep reading source without bound.
type gitBoundedOutput struct {
	buffer          bytes.Buffer
	limit           int
	exceeded        bool
	fieldsRemaining int
	recordsReached  bool
	cancel          context.CancelFunc
}

// Keep Buffer named: promoting its ReaderFrom would let io.Copy bypass Write
// and silently defeat both byte and record bounds.
func (b *gitBoundedOutput) Len() int       { return b.buffer.Len() }
func (b *gitBoundedOutput) String() string { return b.buffer.String() }

func (b *gitBoundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.recordsReached {
		return n, nil
	}
	if b.fieldsRemaining > 0 {
		for i, value := range p {
			if value == 0 {
				b.fieldsRemaining--
				if b.fieldsRemaining == 0 {
					p = p[:i+1]
					b.recordsReached = true
					if b.cancel != nil {
						b.cancel()
					}
					break
				}
			}
		}
	}
	if b.limit <= 0 {
		return b.buffer.Write(p)
	}
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		if !b.exceeded && !b.recordsReached && b.cancel != nil {
			b.cancel()
		}
		b.exceeded = true
		p = p[:remaining]
	}
	_, err := b.buffer.Write(p)
	return n, err
}

// objectStream owns one read-only Git child. Closing it releases both pipe and
// child; it never writes objects, references, index or working-tree files.
type objectStream struct {
	io.ReadCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
	once   sync.Once
	err    error
}

func (s *objectStream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		s.once.Do(func() { s.err = s.cmd.Wait(); s.cancel(); _ = s.ReadCloser.Close() })
		if s.err != nil {
			return n, s.err
		}
	}
	return n, err
}
func (s *objectStream) Close() error {
	s.once.Do(func() { s.cancel(); _ = s.ReadCloser.Close(); s.err = s.cmd.Wait() })
	return s.err
}
func (r runner) openRead(ctx context.Context, args ...string) (io.ReadCloser, error) {
	if r.binary == "" || r.dir == "" {
		return nil, ErrUninitialized
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	// #nosec G204 -- same validated binary and screened argument boundary as runRawRecordLimit.
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = r.dir
	cmd.Env = childEnv()
	cmd.WaitDelay = waitDelay
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		_ = pipe.Close()
		return nil, err
	}
	return &objectStream{ReadCloser: pipe, cmd: cmd, cancel: cancel}, nil
}

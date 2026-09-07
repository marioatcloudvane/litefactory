package git

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel conditions. They exist so a caller can tell the failed check by name
// — "not a git repository" and "no origin remote" are different messages to the
// user — without matching on git's prose.
var (
	// ErrUninitialized reports a [Clone] or [Worktree] that this package did
	// not build. The zero value of either type is deliberately inert rather
	// than silently running git in the process working directory.
	ErrUninitialized = errors.New("git: value was not produced by this package")

	// ErrNotARepository reports a path that is not inside a git repository.
	ErrNotARepository = errors.New("not a git repository")

	// ErrBareRepository reports a repository with no working tree. litefactory
	// registers clones people work in, and a bare repository has no branch,
	// status or worktree to leave untouched.
	ErrBareRepository = errors.New("bare repository, expected a working clone")

	// ErrNotDirectory reports a path that exists but is not a directory.
	ErrNotDirectory = errors.New("not a directory")

	// ErrRemoteNotFound reports a remote that the repository does not define.
	ErrRemoteNotFound = errors.New("remote not found")

	// ErrRevisionNotFound reports a revision that does not resolve.
	ErrRevisionNotFound = errors.New("revision not found")

	// ErrRefNotFound reports a ref that does not exist.
	ErrRefNotFound = errors.New("ref not found")

	// ErrConfigKeyNotFound reports a configuration key that is not set.
	ErrConfigKeyNotFound = errors.New("configuration key not set")

	// ErrWorktreeUncommittedChanges reports a worktree that still holds
	// uncommitted work. Removal refuses rather than discarding it; the caller
	// re-runs with the force option once the user has decided. It is what
	// [*WorktreeDirtyError] unwraps to, so "task clean" can recognise the
	// condition with errors.Is and still reach the file list through errors.As.
	ErrWorktreeUncommittedChanges = errors.New("worktree has uncommitted changes")

	// ErrWorktreeOccupied reports a worktree path that already holds something
	// that is not this task's worktree. It is what [*WorktreeConflictError]
	// unwraps to. Nothing at the path is ever deleted or overwritten to make
	// room, so this condition is always a refusal and never a cleanup.
	ErrWorktreeOccupied = errors.New("worktree path is occupied")

	// ErrWorktreeOverlapsClone reports an attempt to place a worktree inside
	// the registered clone. This is the runtime half of the compile-time
	// guarantee: a [Worktree] can never be made to point at the clone.
	ErrWorktreeOverlapsClone = errors.New("overlaps the registered clone")

	// ErrUpstreamNotRecorded reports a push that git exited 0 from without
	// leaving branch.<name>.remote behind. It is what [*PushError] unwraps to
	// when only the upstream failed.
	//
	// This is a measured condition, not a defensive one. At parallelism 4 on
	// git 2.50.1, 240 concurrent "push --set-upstream" runs from one clone
	// produced 0 non-zero exits and put all 240 branches on the remote, while
	// 89 of them printed "error: could not lock config file .git/config: File
	// exists" and 60 finished with no branch.<name>.remote entry: git loses
	// the race for the config lock, skips the write, and exits 0 anyway. The
	// objects are on the remote; the bookkeeping is what is missing.
	ErrUpstreamNotRecorded = errors.New("push did not record the upstream")

	// ErrInvalidArgument reports a value that would reach git as an option or
	// that git cannot express as an argument.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrUnsupportedForge reports a remote hosted somewhere other than GitHub.
	ErrUnsupportedForge = errors.New("only GitHub is supported")

	// ErrMalformedRemoteURL reports a remote URL that is not in any form this
	// package recognises.
	ErrMalformedRemoteURL = errors.New("unrecognised remote URL")

	// ErrMergeConflict reports a merge that git could not complete on its own.
	// [Worktree.MergeBase] returns it after aborting the in-progress merge and
	// re-verifying the branch tip is back where it started, so a caller never
	// has to distinguish "conflicted" from "conflicted and left half-merged" —
	// only the former is reachable. A caller that wants to know in advance
	// whether a merge would hit this should call [Worktree.MergePreview]
	// first, which reports the same condition without touching the branch.
	ErrMergeConflict = errors.New("merge conflict")

	// ErrUnsupportedGitVersion reports a resolved git older than the 2.38
	// floor "git merge-tree --write-tree" needs. [Worktree.MergePreview]
	// returns it without ever invoking git, so the failure is this named
	// condition and a clear operator sentence rather than a raw
	// [*CommandError] carrying whatever "unknown option" text that git
	// version happens to print.
	ErrUnsupportedGitVersion = errors.New("git version does not support merge-tree --write-tree (requires git 2.38 or newer)")
)

// CommandError reports a git invocation that failed.
//
// The command line and stderr are fields rather than an already-formatted
// message because several acceptance criteria require printing precisely those
// two things, and a caller should not have to reverse-engineer them out of a
// string.
type CommandError struct {
	// Binary is the git executable that ran.
	Binary string
	// Dir is the working directory the command ran in.
	Dir string
	// Args are the arguments passed to git, without the binary itself.
	Args []string
	// ExitCode is git's exit status, or -1 when the process never ran or was
	// killed by a signal.
	ExitCode int
	// Stderr is everything git wrote to its standard error, trailing
	// whitespace removed.
	Stderr string
	// TimedOut reports that the per-command deadline fired, as opposed to the
	// caller's context being cancelled or git exiting non-zero.
	TimedOut bool
	// Timeout is the deadline that fired, meaningful only when TimedOut.
	Timeout time.Duration
	// Err is the underlying cause: an *exec.ExitError, a context error, or one
	// of this package's sentinels once the failure has been classified.
	Err error
}

// Command returns the exact command line that was run.
func (e *CommandError) Command() string {
	binary := e.Binary
	if binary == "" {
		binary = binaryName
	}
	return strings.Join(append([]string{binary}, e.Args...), " ")
}

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

// classify restates a plain non-zero exit as a named condition, keeping the
// command line and stderr intact. A timeout is never reclassified: "git could
// not read the remote" and "git never answered" are different problems and
// must stay distinguishable.
func classify(err error, sentinel error, codes ...int) error {
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.TimedOut {
		return err
	}
	matched := false
	for _, code := range codes {
		if cmdErr.ExitCode == code {
			matched = true
			break
		}
	}
	if !matched {
		return err
	}
	classified := *cmdErr
	classified.Err = sentinel
	return &classified
}

// BinaryNotFoundError reports that git could not be resolved at startup. It
// names the PATH that was searched, because the answer is almost always that
// the daemon's PATH is launchd's four directories and not the shell's.
type BinaryNotFoundError struct {
	// Name is what was looked for.
	Name string
	// PATH is the search path that was used.
	PATH string
	// Err is the underlying lookup or execution failure.
	Err error
}

func (e *BinaryNotFoundError) Error() string {
	return fmt.Sprintf("%s is not usable (searched PATH=%s): %v", e.Name, e.PATH, e.Err)
}

func (e *BinaryNotFoundError) Unwrap() error { return e.Err }

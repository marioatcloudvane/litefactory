package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Clone is a repository that already exists on the user's disk: the one they
// registered, the one they work in, the one litefactory must never write into.
//
// The method set is the whole of S-6 and nothing more:
//
//	Fetch  WorktreeAdd  WorktreeList  WorktreeRemove
//	RevParse  RemoteURL  ConfigGet  ShowRef  ReadStackOverrides
//
// ReadStackOverrides is the bounded stack-admission read: rev-parse verifies an
// exact commit, ls-tree checks directory/file modes and cat-file reads its one
// declared override blob. It changes no checkout, index, object or reference.
//
// There is deliberately no Commit, and no Checkout, Switch, Stash, Reset, Merge
// or Pull either. An engineer who tries to commit to the registered clone gets
// a compile error, which is a guarantee that survives refactors in a way a
// prose rule and a unit test do not. Every mutating operation lives on
// [Worktree], and the only way to obtain one is [Clone.WorktreeAdd].
//
// A Clone is safe for concurrent use: it holds no mutable state, and each
// method uses only its declared git operations. That is a statement about this Go value and not
// about the repository on disk it points at, and conflating the two is the
// mistake this paragraph exists to prevent. git keeps its own mutable state in
// the .git directory, and against *one* clone the methods here contend on it:
// [Clone.Fetch] on refs/remotes/*, [Clone.WorktreeAdd] on the shared registry
// under .git/worktrees, and [Worktree.Push] — a linked worktree shares the
// clone's config — on .git/config.
//
// That contention is measured, not feared. At parallelism 4, with
// litefactory's own argv: fetch failed 75% of the time whenever the remote's
// refs had actually moved ("cannot lock ref"), worktree add failed 0.75%
// fatally on a sibling's half-written registry entry, and "push
// --set-upstream" left 25% of branches with no upstream set at all — while
// exiting 0 every single time.
//
// A "could not lock config file .git/config" error was printed in more runs
// still (37%) than ended up missing an upstream (25%). The gap between those
// two numbers was never chased: both are bad, and the exact accounting does
// not change what a caller has to do about it.
//
// The silent one is the dangerous one. The other two are loud enough that a
// caller can retry or report them; that one returns nil from a call that half
// failed, and the missing half is not noticed until something downstream needs
// the upstream to be there.
//
// Serialising these is the caller's job. This package declares no mutex and
// deliberately will not: the correct granularity is "one clone", and nothing
// here can know how many clones a caller has or which two goroutines are about
// to share one. The serialisation that exists is the per-clone gate in
// internal/daemon (cloneGate, keyed on the registered repository's path),
// acquired by internal/turn and held across a whole critical section rather
// than around single calls. It is named here so that a reader can go and find
// it, and deliberately not imported: internal/git is a leaf package, and it
// stays one.
type Clone struct {
	runner runner
	path   string
}

// Open resolves dir and returns the repository containing it.
//
// The path is made absolute and symlink-resolved, then handed to git, and the
// repository's own top level is what the returned Clone reports — so a
// subdirectory resolves to the repository that owns it, and two spellings of
// the same repository produce the same path. A path that does not exist, is not
// a directory, is not in a repository, or is a bare repository is rejected with
// a named condition so the caller can say which check failed.
//
// This function runs only local plumbing: opening a clone makes no network
// call.
func (b *Binary) Open(ctx context.Context, dir string) (*Clone, error) {
	if b.Path() == "" {
		return nil, ErrUninitialized
	}
	start, err := resolveExisting(dir)
	if err != nil {
		return nil, err
	}

	probe := runner{binary: b.path, dir: start, mergeTreeSupported: b.mergeTreeSupported}
	// --is-bare-repository is asked first because it answers in a bare
	// repository, where --show-toplevel fails with a message about work trees
	// that would send the user looking in the wrong direction.
	bare, err := probe.run(ctx, readTimeout, "rev-parse", "--is-bare-repository")
	if err != nil {
		return nil, classify(err, ErrNotARepository, 128)
	}
	if bare == "true" {
		return nil, fmt.Errorf("%s: %w", start, ErrBareRepository)
	}

	top, err := probe.run(ctx, readTimeout, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(top)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root %s: %w", top, err)
	}
	return &Clone{runner: runner{binary: b.path, dir: root, mergeTreeSupported: b.mergeTreeSupported}, path: root}, nil
}

// Path returns the repository's top level: absolute, symlink-resolved and
// canonical. It reads a field rather than running git, so it is not part of the
// S-6 command allowlist.
func (c *Clone) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Fetch runs "git fetch --prune" against remote. It updates remote-tracking
// refs and deletes the ones whose upstream is gone; it does not touch the
// working tree, the index or HEAD.
//
// This is the one method here that talks to the network, and it carries a 60
// second deadline: without it a passphrase-protected key produces a hung tick
// rather than an error, and a hung daemon looks alive.
// Explicit branch coverage makes successful fetch authoritative even when the
// clone has a restrictive configured fetch refspec. Pruning covers this namespace.
func (c *Clone) Fetch(ctx context.Context, remote string) error {
	if err := checkName("remote", remote); err != nil {
		return err
	}
	_, err := c.runner.run(ctx, fetchTimeout, "fetch", "--prune", "--", remote, "+refs/heads/*:refs/remotes/"+remote+"/*")
	return err
}

// WorktreeAddOptions configures [Clone.WorktreeAdd].
type WorktreeAddOptions struct {
	// Branch is the branch to check out in the new worktree. Required.
	Branch string
	// CreateFrom, when set, creates Branch at that start point — typically
	// "origin/<base>". Empty means Branch must already exist.
	//
	// Creating the branch as part of "git worktree add" is why the S-6
	// allowlist needs no "git branch": the new ref is made without the
	// registered clone's own working tree or HEAD moving.
	CreateFrom string
	// Detach checks Branch out with a detached HEAD instead; Branch must then
	// be a full commit object id, and no ref is created or moved. It is for
	// short-lived worktrees that only read a fixed commit, such as the target
	// tree a Bug check reruns failing tests on.
	Detach bool
}

// WorktreeAdd creates a linked worktree at dir and returns the only kind of
// handle that can write to a repository.
//
// dir is made absolute and its existing ancestors symlink-resolved before it is
// compared against the clone: a path inside the registered clone — or one
// containing it — is refused with [ErrWorktreeOverlapsClone]. The handle is
// built before git runs, so that refusal happens without a single command being
// issued. There is no force option, so an occupied path is a loud failure from
// git and never a delete or an overwrite. Missing parent directories are
// created by git itself.
//
// This is the low-level constructor: it always adds. Callers that want "reuse
// what is already there, otherwise create it" want [EnsureWorktree].
func (c *Clone) WorktreeAdd(ctx context.Context, dir string, opts WorktreeAddOptions) (*Worktree, error) {
	target, err := resolveTarget(dir)
	if err != nil {
		return nil, err
	}
	worktree, err := c.newWorktree(target, opts.Branch)
	if err != nil {
		return nil, err
	}

	args := []string{"worktree", "add"}
	switch {
	case opts.Detach:
		if opts.CreateFrom != "" || !fullBugObject(opts.Branch) {
			return nil, fmt.Errorf("detached worktree: %w: needs a full commit id and no new branch", ErrInvalidArgument)
		}
		args = append(args, "--detach", target, opts.Branch)
	case opts.CreateFrom == "":
		args = append(args, target, opts.Branch)
	default:
		if err := checkName("start point", opts.CreateFrom); err != nil {
			return nil, err
		}
		args = append(args, "-b", opts.Branch, target, opts.CreateFrom)
	}
	if _, err := c.runner.run(ctx, worktreeTimeout, args...); err != nil {
		return nil, err
	}
	return worktree, nil
}

// WorktreeInfo describes one entry of "git worktree list".
type WorktreeInfo struct {
	// Path is the worktree's root directory.
	Path string
	// Head is the object id HEAD points at, empty for a bare repository.
	Head string
	// Branch is the full ref name checked out there, for example
	// "refs/heads/lf/12-thing". Empty when HEAD is detached.
	Branch string
	// Bare reports the entry as a bare repository.
	Bare bool
	// Detached reports HEAD not being on a branch.
	Detached bool
	// Locked reports a worktree git refuses to prune or remove.
	Locked bool
	// LockReason is the message recorded with the lock, if any.
	LockReason string
	// Prunable reports a worktree whose directory git believes is gone.
	Prunable bool
}

// WorktreeList reports the repository's worktrees, the registered clone itself
// first.
//
// It returns data, not [Worktree] values, and that is deliberate: the first
// entry is the registered clone, and handing back a Worktree for it would open
// exactly the hole this package exists to close. Callers that need to act on a
// worktree get it from [Clone.WorktreeAdd].
//
// Parsing uses the NUL-delimited porcelain format, so a path containing a
// newline cannot forge an entry. That format needs git 2.36 or newer.
func (c *Clone) WorktreeList(ctx context.Context) ([]WorktreeInfo, error) {
	out, err := c.runner.run(ctx, readTimeout, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out), nil
}

// parseWorktreeList decodes "git worktree list --porcelain -z". Attributes are
// NUL-terminated and an empty attribute separates entries.
func parseWorktreeList(out string) []WorktreeInfo {
	var (
		list    []WorktreeInfo
		current *WorktreeInfo
	)
	flush := func() {
		if current != nil {
			list = append(list, *current)
			current = nil
		}
	}
	for _, attr := range strings.Split(out, "\x00") {
		if attr == "" {
			flush()
			continue
		}
		key, value, _ := strings.Cut(attr, " ")
		if key == "worktree" {
			flush()
			current = &WorktreeInfo{Path: value}
			continue
		}
		if current == nil {
			continue
		}
		switch key {
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = value
		case "bare":
			current.Bare = true
		case "detached":
			current.Detached = true
		case "locked":
			current.Locked = true
			current.LockReason = value
		case "prunable":
			current.Prunable = true
		}
	}
	flush()
	return list
}

// WorktreeRemoveOptions configures [Clone.WorktreeRemove].
type WorktreeRemoveOptions struct {
	// Force removes the worktree even when it holds uncommitted changes. It
	// exists for "task clean --force" and must never be the default: the
	// unforced call is what makes losing work impossible by accident.
	Force bool
}

// WorktreeRemove deletes a linked worktree and its administrative entry. It is
// what "litefactory task clean <issue>" runs.
//
// The path is refused if it overlaps the registered clone. git rejects removing
// the main working tree on its own, but the check is written down here too
// because "the registered clone is never deleted" is a guarantee this package
// owns rather than one it borrows.
//
// Without Force, a worktree holding uncommitted changes is refused with
// [*WorktreeDirtyError] and nothing is removed. git refuses that case too, but
// only with a sentence on stderr; naming the condition — and listing the files
// that would have been lost — is what lets the CLI tell the user what to do
// instead of relaying a fatal.
func (c *Clone) WorktreeRemove(ctx context.Context, dir string, opts WorktreeRemoveOptions) error {
	target, err := resolveTarget(dir)
	if err != nil {
		return err
	}
	if overlaps(c.Path(), target) {
		return fmt.Errorf("worktree path %s %w %s", target, ErrWorktreeOverlapsClone, c.Path())
	}
	if !opts.Force {
		if changes := c.uncommittedChanges(ctx, target); len(changes) > 0 {
			return &WorktreeDirtyError{Path: target, Changes: changes}
		}
	}
	args := []string{"worktree", "remove"}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, target)
	_, err = c.runner.run(ctx, worktreeTimeout, args...)
	return err
}

// uncommittedChanges lists what is uncommitted in the worktree at target, for
// the unforced-removal refusal above.
//
// It is a probe, not a check: a target that is not a worktree at all, or that
// git will not talk about, reports nothing and lets "git worktree remove" — the
// actual authority on what target is — produce the error. Turning a failed
// probe into a refusal would block removing a broken worktree, which is exactly
// what "task clean" exists for.
func (c *Clone) uncommittedChanges(ctx context.Context, target string) []FileChange {
	probe := runner{binary: c.runner.binary, dir: target}
	out, err := probe.run(ctx, readTimeout, statusArgs...)
	if err != nil {
		return nil
	}
	changes, err := parseStatus(out)
	if err != nil {
		return nil
	}
	return changes
}

// WorktreeDirtyError reports an unforced removal refused because the worktree
// still holds work. It carries the changes rather than a count so the CLI can
// print what the user would have lost.
type WorktreeDirtyError struct {
	// Path is the worktree that was left alone.
	Path string
	// Changes are the uncommitted changes found there.
	Changes []FileChange
}

func (e *WorktreeDirtyError) Error() string {
	return fmt.Sprintf("worktree %s has %d uncommitted change(s) and was not removed: %v",
		e.Path, len(e.Changes), ErrWorktreeUncommittedChanges)
}

func (e *WorktreeDirtyError) Unwrap() error { return ErrWorktreeUncommittedChanges }

// RevParseOptions configures [Clone.RevParse] and [Worktree.RevParse].
type RevParseOptions struct {
	// AbbrevRef returns the symbolic name a revision resolves to instead of
	// its object id: "HEAD" becomes the current branch, "origin/HEAD" becomes
	// "origin/main". This is how the remote's default branch is read without
	// reaching outside the S-6 allowlist for "git symbolic-ref".
	AbbrevRef bool
}

// RevParse resolves rev, returning its object id or — with AbbrevRef — its
// symbolic name. A revision that does not resolve gives [ErrRevisionNotFound].
func (c *Clone) RevParse(ctx context.Context, rev string, opts RevParseOptions) (string, error) {
	return revParse(ctx, c.runner, rev, opts)
}

// revParse is shared by Clone and Worktree; the command is identical and only
// the directory differs.
func revParse(ctx context.Context, r runner, rev string, opts RevParseOptions) (string, error) {
	if err := checkName("revision", rev); err != nil {
		return "", err
	}
	args := []string{"rev-parse"}
	if opts.AbbrevRef {
		args = append(args, "--abbrev-ref")
	}
	// --end-of-options stops git reading a revision that starts with a dash as
	// an option. checkName already refuses those, so this is the second lock
	// on the same door.
	args = append(args, "--verify", "--end-of-options", rev)

	out, err := r.run(ctx, readTimeout, args...)
	if err != nil {
		return "", classify(err, ErrRevisionNotFound, 128)
	}
	if out == "" {
		return "", fmt.Errorf("revision %q: %w", rev, ErrRevisionNotFound)
	}
	return out, nil
}

// RemoteURL returns the URL configured for remote, reading only local config —
// this is the "no network call" half of repo registration. A remote that is not
// configured gives [ErrRemoteNotFound].
//
// A remote with several URLs reports the first, which is the one git fetches
// from.
func (c *Clone) RemoteURL(ctx context.Context, remote string) (string, error) {
	if err := checkName("remote", remote); err != nil {
		return "", err
	}
	out, err := c.runner.run(ctx, readTimeout, "remote", "get-url", "--", remote)
	if err != nil {
		return "", classify(err, ErrRemoteNotFound, 2, 128)
	}
	url, _, _ := strings.Cut(out, "\n")
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("remote %q: %w", remote, ErrRemoteNotFound)
	}
	return url, nil
}

// ConfigGet returns the value of a configuration key as the repository sees it,
// including anything inherited from the user's global config. A key that is not
// set gives [ErrConfigKeyNotFound]; that is a normal answer, not a failure.
func (c *Clone) ConfigGet(ctx context.Context, key string) (string, error) {
	if err := checkName("configuration key", key); err != nil {
		return "", err
	}
	out, err := c.runner.run(ctx, readTimeout, "config", "--get", "--", key)
	if err != nil {
		return "", classify(err, ErrConfigKeyNotFound, 1)
	}
	return out, nil
}

// ShowRef returns the object id of a fully qualified ref, for example
// "refs/heads/lf/12-thing" or "refs/remotes/origin/main". A ref that does not
// exist gives [ErrRefNotFound], which is how "does this branch already exist,
// locally or on the remote" is answered without creating anything.
func (c *Clone) ShowRef(ctx context.Context, ref string) (string, error) {
	if err := checkName("ref", ref); err != nil {
		return "", err
	}
	out, err := c.runner.run(ctx, readTimeout, "show-ref", "--verify", "--", ref)
	if err != nil {
		// Non-quiet verification uses fatal exit 128 for an absent ref too.
		// Confirm absence with quiet verification (exit 1), preserving the
		// original command diagnostics while refusing real repository failures.
		_, verifyErr := c.runner.run(ctx, readTimeout, "show-ref", "--verify", "--quiet", "--", ref)
		if errors.Is(classify(verifyErr, ErrRefNotFound, 1), ErrRefNotFound) {
			return "", classify(err, ErrRefNotFound, 1, 128)
		}
		return "", err
	}
	id, _, ok := strings.Cut(out, " ")
	if !ok || id == "" {
		return "", fmt.Errorf("invalid observation of ref %q", ref)
	}
	return id, nil
}

// resolveExisting makes path absolute and symlink-resolved, and requires it to
// be an existing directory.
func resolveExisting(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("repository path: %w: must not be empty", ErrInvalidArgument)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", abs, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s: %w", resolved, ErrNotDirectory)
	}
	return resolved, nil
}

// resolveTarget makes path absolute and resolves symlinks as far down as the
// filesystem goes, leaving the not-yet-created tail alone. A worktree path does
// not exist yet, but it still has to be compared against the clone's real path
// — otherwise a symlinked parent would let a worktree land inside the
// registered clone while looking like it was somewhere else entirely.
func resolveTarget(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("worktree path: %w: must not be empty", ErrInvalidArgument)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	dir, tail := filepath.Clean(abs), ""
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(resolved, tail), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Walked up to the filesystem root without finding anything that
			// exists, which should be impossible.
			return "", fmt.Errorf("resolve %s: %w", abs, err)
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = parent
	}
}

// overlaps reports whether two absolute paths are the same directory or one
// contains the other.
func overlaps(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return a == b || under(a, b) || under(b, a)
}

// under reports whether path lies beneath root.
func under(path, root string) bool {
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

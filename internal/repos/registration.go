package repos

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// OriginRemote is the only remote litefactory reads. A clone without it is
// refused rather than guessed at: the origin URL is where the owner, the name
// and the forge all come from.
const OriginRemote = "origin"

// remoteHEADRev is the *local* ref that records which branch the remote
// considers its default — refs/remotes/origin/HEAD, written by clone and by
// "git remote set-head". Reading it is what lets base-branch resolution answer
// without a network round trip.
const remoteHEADRev = OriginRemote + "/HEAD"

// remoteRefPrefix is what "git rev-parse --abbrev-ref origin/HEAD" puts in
// front of the branch name: "origin/main".
const remoteRefPrefix = OriginRemote + "/"

// Source names where a resolved value came from. It exists so `repo add` can
// tell the user *why* it chose a base branch or a worktree root — the three
// sources of each are documented behaviour (AC-1.3, AC-1.6), and silently
// picking one of them is how a surprising registration goes unnoticed.
type Source string

const (
	// SourceFlag is a value given on the command line.
	SourceFlag Source = "flag"
	// SourceConfig is a value from config.yaml.
	SourceConfig Source = "config"
	// SourceRemoteHEAD is a base branch read from the locally cached
	// refs/remotes/origin/HEAD.
	SourceRemoteHEAD Source = "remote HEAD"
	// SourceSiblingDefault is the S-6 worktree root beside the clone.
	SourceSiblingDefault Source = "sibling default"
)

// Options are the per-invocation overrides `repo add` accepts as flags. An
// empty field means "not given", which is how the resolution order below falls
// through to the next source.
type Options struct {
	// BaseBranch is --base.
	BaseBranch string
	// WorktreeRoot is --worktrees.
	WorktreeRoot string
	// FeaturePrefix is --prefix.
	FeaturePrefix string
}

// Defaults are the global settings from config.yaml that registration falls
// back to. The caller fills them in; this package does not read config.yaml,
// so a test can drive every branch of the resolution order without a file.
type Defaults struct {
	// BaseBranch is default_base_branch. Empty is valid and means the remote
	// HEAD has to answer.
	BaseBranch string
	// WorktreeRoot is the global worktree_root. Empty means the S-6 sibling
	// default.
	WorktreeRoot string
	// FeaturePrefix is feature_prefix.
	FeaturePrefix string
}

// Registration is a clone that has passed every check, with every derived
// value already decided. It is the whole result of inspecting a repository,
// and it is a plain value: nothing here has been written to disk or to the
// database yet.
type Registration struct {
	// Path is the clone's top level: absolute, symlink-resolved, canonical.
	// It is the unique key the repos table is upserted on (AC-1.8).
	Path string
	// Ref is forge:owner/name, parsed from the origin URL.
	Ref git.RepoRef
	// RemoteURL is the origin URL exactly as the clone has it configured.
	RemoteURL string
	// BaseBranch is the branch task branches will be cut from, and
	// BaseBranchSource says which of the three sources supplied it.
	BaseBranch       string
	BaseBranchSource Source
	// FeaturePrefix is the first segment of every task branch.
	FeaturePrefix string
	// WorktreeRoot is the absolute directory worktrees will be created under,
	// and WorktreeRootSource says which of the three sources supplied it.
	WorktreeRoot       string
	WorktreeRootSource Source
}

// StoreRepo renders the registration as the row to upsert.
//
// LastFetchAt is deliberately left at the zero time. It belongs to
// [store.DB.TouchRepoFetch], and registration must never make litefactory
// believe it has fetched.
func (r Registration) StoreRepo() store.Repo {
	return store.Repo{
		Path:          r.Path,
		Forge:         r.Ref.Forge,
		Owner:         r.Ref.Owner,
		Name:          r.Ref.Name,
		RemoteURL:     r.RemoteURL,
		BaseBranch:    r.BaseBranch,
		FeaturePrefix: r.FeaturePrefix,
		WorktreeRoot:  r.WorktreeRoot,
	}
}

// Resolve inspects the clone at path and decides everything `repo add` needs
// to store about it.
//
// The checks run in the order a user would want to hear about them, and the
// first failure stops the rest: the path is a working clone, it has an origin,
// the origin is a forge we speak, a base branch can be
// determined, the branch prefix is legal, and a worktree root resolves. Every
// failure is a [*CheckError] naming which of those it was (AC-1.1), wrapping a
// cause that is either one of this package's sentinels or one of
// [github.com/cloudvane/litefactory/internal/git]'s — including
// [*git.CommandError], which carries the exact command line and stderr.
//
// Nothing is written inside the clone and nothing goes over the network. Both
// are structural rather than careful: every git call goes through [git.Clone],
// which has no mutating method at all, and whose only networked method is
// Fetch — which is not called here and has no reason to be.
func Resolve(ctx context.Context, binary *git.Binary, path string, opts Options, def Defaults) (*Registration, error) {
	clone, err := openClone(ctx, binary, path)
	if err != nil {
		return nil, err
	}
	root := clone.Path()

	remoteURL, err := clone.RemoteURL(ctx, OriginRemote)
	if err != nil {
		return nil, &CheckError{Check: CheckOriginRemote, Path: root, Err: err}
	}
	ref, err := git.ParseRemoteURL(remoteURL)
	if err != nil {
		return nil, &CheckError{Check: CheckSupportedForge, Path: root, Err: err}
	}

	base, baseSource, err := resolveBaseBranch(ctx, clone, opts.BaseBranch, def.BaseBranch)
	if err != nil {
		return nil, &CheckError{Check: CheckBaseBranch, Path: root, Err: err}
	}

	prefix, err := resolveFeaturePrefix(opts.FeaturePrefix, def.FeaturePrefix)
	if err != nil {
		return nil, &CheckError{Check: CheckFeaturePrefix, Path: root, Err: err}
	}

	worktreeRoot, rootSource, err := resolveWorktreeRoot(root, opts.WorktreeRoot, def.WorktreeRoot)
	if err != nil {
		return nil, &CheckError{Check: CheckWorktreeRoot, Path: root, Err: err}
	}

	return &Registration{
		Path:               root,
		Ref:                ref,
		RemoteURL:          remoteURL,
		BaseBranch:         base,
		BaseBranchSource:   baseSource,
		FeaturePrefix:      prefix,
		WorktreeRoot:       worktreeRoot,
		WorktreeRootSource: rootSource,
	}, nil
}

// openClone opens the repository containing path, restating git's refusal as a
// named check.
//
// [git.Binary.Open] already does the absolute-and-symlink-resolved half of
// AC-1.1, and it reports "does not exist", "not a directory", "not a
// repository" and "bare repository" as distinct conditions. All this adds is
// the check name, so the message says which door was locked instead of
// "invalid repository".
func openClone(ctx context.Context, binary *git.Binary, path string) (*git.Clone, error) {
	clone, err := binary.Open(ctx, path)
	if err == nil {
		return clone, nil
	}
	check := CheckGitRepository
	switch {
	case errors.Is(err, fs.ErrNotExist):
		check = CheckPath
	case errors.Is(err, git.ErrNotDirectory):
		check = CheckDirectory
	}
	return nil, &CheckError{Check: check, Path: path, Err: err}
}

// resolveBaseBranch applies AC-1.3's order exactly: --base, then
// default_base_branch, then the remote's HEAD. Nothing else is consulted — in
// particular there is no "well, main probably exists" fallback, because a
// guessed base branch is discovered as a wrong pull request rather than as an
// error.
func resolveBaseBranch(ctx context.Context, clone *git.Clone, flagValue, configValue string) (string, Source, error) {
	for _, candidate := range []struct {
		value  string
		source Source
	}{
		{strings.TrimSpace(flagValue), SourceFlag},
		{strings.TrimSpace(configValue), SourceConfig},
	} {
		if candidate.value == "" {
			continue
		}
		if err := validateBranchName(candidate.value); err != nil {
			return "", "", err
		}
		return candidate.value, candidate.source, nil
	}

	branch, err := remoteDefaultBranch(ctx, clone)
	switch {
	case errors.Is(err, git.ErrRevisionNotFound):
		// No origin/HEAD here. That is an ordinary state — "git clone
		// --single-branch" and some mirrors never write one — so it is the
		// third source coming up empty, not a git failure to report.
		return "", "", ErrBaseBranchUnknown
	case err != nil:
		return "", "", err
	}
	return branch, SourceRemoteHEAD, nil
}

// remoteDefaultBranch reads the remote's default branch out of the locally
// cached refs/remotes/origin/HEAD.
//
// This is the load-bearing half of "`repo add` makes no network call". The
// obvious implementation is "git ls-remote --symref origin HEAD", and it is
// wrong: it would make registering a repository fail on a plane, behind a
// captive portal, or whenever the SSH agent is locked. rev-parse reads a file
// in .git and answers offline. It is also why [git.Clone] exposes no
// ls-remote — there is nothing here to reach for.
func remoteDefaultBranch(ctx context.Context, clone *git.Clone) (string, error) {
	name, err := clone.RevParse(ctx, remoteHEADRev, git.RevParseOptions{AbbrevRef: true})
	if err != nil {
		return "", err
	}
	branch := strings.TrimPrefix(name, remoteRefPrefix)
	if branch == "" || branch == name {
		return "", fmt.Errorf("%s resolved to %q, which does not name a branch on %q: %w",
			remoteHEADRev, name, OriginRemote, git.ErrRevisionNotFound)
	}
	if err := validateBranchName(branch); err != nil {
		return "", err
	}
	return branch, nil
}

// resolveFeaturePrefix applies --prefix, then feature_prefix, then the
// package-wide default, and validates whatever wins. The prefix becomes a
// branch component on every task branch, so an illegal one has to fail here
// rather than at the first worktree.
func resolveFeaturePrefix(flagValue, configValue string) (string, error) {
	prefix := strings.TrimSpace(flagValue)
	if prefix == "" {
		prefix = strings.TrimSpace(configValue)
	}
	if prefix == "" {
		prefix = naming.DefaultFeaturePrefix
	}
	if err := naming.ValidateFeaturePrefix(prefix); err != nil {
		return "", err
	}
	return prefix, nil
}

// resolveWorktreeRoot applies S-6's order: --worktrees, then the global
// worktree_root, then the sibling default beside the clone.
//
// The result is always absolute (AC-1.6). Storing a relative root would tie
// every future worktree to whichever directory `repo add` happened to be run
// from — and the daemon that later reads the row runs under launchd, whose
// working directory is not that one.
func resolveWorktreeRoot(clonePath, flagValue, configValue string) (string, Source, error) {
	for _, candidate := range []struct {
		value  string
		source Source
	}{
		{strings.TrimSpace(flagValue), SourceFlag},
		{strings.TrimSpace(configValue), SourceConfig},
	} {
		if candidate.value == "" {
			continue
		}
		abs, err := filepath.Abs(candidate.value)
		if err != nil {
			return "", "", fmt.Errorf("%w: %q: %w", ErrInvalidWorktreeRoot, candidate.value, err)
		}
		return filepath.Clean(abs), candidate.source, nil
	}

	root, err := naming.DefaultWorktreeRoot(clonePath)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidWorktreeRoot, err)
	}
	return root, SourceSiblingDefault, nil
}

// branchNameRE is the shape of a branch name litefactory will accept. It is
// the same expression internal/config validates default_base_branch with, so a
// value that survives config validation also survives here.
var branchNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// validateBranchName screens a base branch before it is stored.
//
// It is deliberately narrower than git-check-ref-format. git accepts names
// this rejects, but every one of them — a leading dash, an embedded "..", a
// trailing "/" — is either an argument-injection shape or a name nobody names
// a default branch, and a stored base branch is concatenated into refs like
// "origin/<base>" for the lifetime of the repository.
func validateBranchName(name string) error {
	fail := func(reason string) error {
		return fmt.Errorf("%w: %q %s", ErrInvalidBaseBranch, name, reason)
	}
	switch {
	case name == "":
		return fail("is empty")
	case !branchNameRE.MatchString(name):
		return fail("must start with a letter or digit and contain only letters, digits, dot, dash, slash or underscore")
	case strings.Contains(name, ".."), strings.Contains(name, "//"):
		return fail(`contains ".." or "//", which git will not resolve`)
	case strings.HasSuffix(name, "/"), strings.HasSuffix(name, "."):
		return fail("ends with a separator")
	case strings.HasSuffix(name, ".lock"):
		return fail(`ends in ".lock", which git reserves`)
	}
	return nil
}

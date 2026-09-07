package repos

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// The named checks `repo add` runs, in the order it runs them. They are
// exported because AC-1.1 asks for a message that says which check failed
// rather than a generic "invalid repository", and because a caller mapping the
// failure to an exit code should not have to match on prose.
const (
	// CheckPath is "the path exists".
	CheckPath = "path exists"
	// CheckDirectory is "the path is a directory".
	CheckDirectory = "path is a directory"
	// CheckGitRepository is "the path is inside a non-bare git repository".
	CheckGitRepository = "git repository"
	// CheckOriginRemote is "the repository has an origin remote".
	CheckOriginRemote = "origin remote"
	// CheckSupportedForge is "the origin URL names a forge litefactory speaks".
	CheckSupportedForge = "supported forge"
	// CheckAiteam is "aiteam is installed in the repository".
	CheckAiteam = "aiteam installed"
	// CheckBaseBranch is "a base branch could be determined".
	CheckBaseBranch = "base branch"
	// CheckFeaturePrefix is "the branch prefix is one git would accept".
	CheckFeaturePrefix = "feature prefix"
	// CheckWorktreeRoot is "a worktree root could be resolved, absolutely".
	CheckWorktreeRoot = "worktree root"
)

// Sentinel conditions. A caller branches on these with errors.Is; the command
// layer is what turns them into exit codes.
var (
	// ErrAiteamMissing reports a repository with no aiteam installation.
	// AC-1.4: the repository is not registered, the message names `npx
	// aiteam`, and the command exits 3.
	ErrAiteamMissing = errors.New(
		"aiteam is not installed here: no CLAUDE.md mentioning aiteam and no .aiteam directory; " +
			"run `npx aiteam` in the repository first (litefactory never installs it for you)")

	// ErrBaseBranchUnknown reports that none of the three sources in AC-1.3
	// produced a base branch.
	ErrBaseBranchUnknown = errors.New(
		"cannot determine the base branch: this clone has no origin/HEAD and default_base_branch is not configured; " +
			"pass --base <branch>")

	// ErrInvalidBaseBranch reports a base branch git would not accept as a
	// branch name.
	ErrInvalidBaseBranch = errors.New("invalid base branch")

	// ErrInvalidWorktreeRoot reports a worktree root that cannot be stored as
	// an absolute path (AC-1.6).
	ErrInvalidWorktreeRoot = errors.New("invalid worktree root")
)

// RepoNotFoundError reports that a `team add` <repo-identifier> ([ResolveIdentifier])
// matched no registered repository. It names the identifier exactly as typed
// (US-19 AC-4), never the resolution path it took to get there.
type RepoNotFoundError struct {
	// Identifier is the raw <repo-identifier> the operator typed.
	Identifier string
}

func (e *RepoNotFoundError) Error() string {
	return fmt.Sprintf("no registered repository matches %q", e.Identifier)
}

// AmbiguousRepoError reports that a `team add` <repo-identifier>
// ([ResolveIdentifier]) matched more than one registered repository. It is a
// typed error rather than a sentinel so the caller can list every candidate's
// full owner/name (US-19 AC-3) — the same shape [store.AmbiguousIssueError]
// uses for the same reason.
type AmbiguousRepoError struct {
	// Identifier is the raw <repo-identifier> the operator typed.
	Identifier string
	// Candidates are every repository the identifier matched, in the order
	// [store.DB.ListRepos] returned them (owner, then name).
	Candidates []store.Repo
}

func (e *AmbiguousRepoError) Error() string {
	names := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		names = append(names, c.Slug())
	}
	return fmt.Sprintf("%q matches more than one registered repository (%s)",
		e.Identifier, strings.Join(names, ", "))
}

// CheckError reports which registration check rejected a path, and why.
//
// Check is one of the exported check constants above and Err is the underlying
// cause — a git sentinel, a parse failure or one of this package's own
// sentinels — so both "which check" and "what went wrong" survive the trip up
// to the command layer.
type CheckError struct {
	// Check names the check that failed.
	Check string
	// Path is the repository path the check ran against, as far as it had been
	// resolved when the check failed.
	Path string
	// Err is the underlying cause.
	Err error
}

func (e *CheckError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s: %v", e.Check, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Path, e.Check, e.Err)
}

func (e *CheckError) Unwrap() error { return e.Err }

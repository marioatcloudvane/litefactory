package repos

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// collisionProbeIssue is the issue number the comparison key is derived from.
// Any positive number answers the same question — see [worktreeKey] — and one
// is the smallest a forge will ever mint.
const collisionProbeIssue = 1

// ErrWorktreeRootCollision reports two repositories that map an issue number
// to one worktree directory. A caller branches on it with errors.Is; the
// command layer is what turns it into an exit code, the way it does for a
// literal secret in config.yaml.
var ErrWorktreeRootCollision = errors.New("two repositories would create their worktrees in the same directory")

// CollidingRepo names one side of a collision. Both the path and the
// owner/name slug are carried because neither on its own is enough to act on:
// the path is what `repo add --worktrees` has to be re-run against, and the
// slug is how the operator thinks of the repository.
type CollidingRepo struct {
	// Path is the registered clone's path, the unique key of its repos row.
	Path string
	// Slug is owner/name.
	Slug string
}

// WorktreeCollisionError reports the pair, and unwraps to
// [ErrWorktreeRootCollision].
type WorktreeCollisionError struct {
	// First and Second are the two repositories, ordered by path so that one
	// configuration always produces one message.
	First, Second CollidingRepo
	// Root is the shared worktree root, cleaned: the two rows may spell it
	// differently and still resolve here.
	Root string
	// Issue and Directory are one worked example — the issue number the check
	// probed and the directory both repositories map it to. Every other issue
	// number collides identically.
	Issue     int
	Directory string
}

func (e *WorktreeCollisionError) Error() string {
	return fmt.Sprintf(
		"worktree path collision: %s (%s) and %s (%s) both create worktrees under %s, "+
			"so issue %d of either lands in %s — as does every other issue number, "+
			"because the worktree path carries the issue number but not the repository. "+
			"Give one of them a worktree root of its own: `litefactory repo add <path> --worktrees <dir>`",
		e.First.Path, e.First.Slug, e.Second.Path, e.Second.Slug,
		e.Root, e.Issue, e.Directory)
}

func (e *WorktreeCollisionError) Unwrap() error { return ErrWorktreeRootCollision }

// ValidateWorktreeRoots refuses a set of registered repositories in which two
// of them map an issue number to one worktree directory (AC-PAR-4.8). It is
// what the daemon runs over the repos table at startup, before it takes the
// singleton lock and therefore before it can run a turn.
//
// # Why the refusal is a security control
//
// Sequentially the collision is an annoyance: the second turn's vacancy check
// fails loudly because the first turn's directory is already there.
// Concurrently it is an exposure. Two turns on two repositories take two
// *different* clone gates and hold two correctly granted session locks on two
// different rows, so nothing else in the design stops them — and "check the
// directory is vacant" then MkdirAll then "worktree add" is a TOCTOU both can
// pass before either directory exists. A correctly authorised turn can land in
// the directory another issue's claude is working in, with that issue's
// content and its diff. Most interleavings still fail loudly at the second
// "worktree add"; "most" is not a security property.
//
// Removing the collision instead of refusing it means putting the repository
// into the worktree path, which would relocate every worktree already on disk.
// That is a migration question of its own and is deliberately deferred:
// [naming.WorktreePath] is unchanged.
//
// # What it reports
//
// Every collision, joined, rather than the first — the same reason config
// validation aggregates. An operator who put three repositories under one
// global worktree_root should learn all of it in one startup instead of one
// repository per restart.
//
// A row whose worktree root will not produce a path is an error too, not a
// skip. It cannot run a turn either way, and a row this check cannot key is a
// row it is not comparing, which is a hole in it.
func ValidateWorktreeRoots(list []store.Repo) error {
	sorted := slices.Clone(list)
	slices.SortFunc(sorted, func(a, b store.Repo) int { return strings.Compare(a.Path, b.Path) })

	var errs []error
	claimed := make(map[string]store.Repo, len(sorted))
	for _, repo := range sorted {
		dir, err := worktreeKey(repo)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s (%s): %w", repo.Path, repo.Slug(), err))
			continue
		}
		if other, taken := claimed[dir]; taken {
			errs = append(errs, collision(other, repo, dir))
			continue
		}
		claimed[dir] = repo
	}
	return errors.Join(errs...)
}

// ValidateNewWorktreeRoot refuses a registration that would collide with an
// already-registered repository (AC-PAR-4.8). `repo add` runs it *before* the
// upsert, so a refused registration leaves no row behind — otherwise a
// colliding repository would be registered into a running daemon and
// discovered only at its next restart, with colliding turns in between.
//
// existing is the whole repos table, supplied by the caller. Reading it is a
// store query, not a network call: registration still makes none, and the
// decision itself stays a pure function of the rows it is handed.
//
// The row with the candidate's own path is excluded. `repo add` on an
// already-registered path updates that row rather than adding a second one
// (AC-1.8), so comparing a registration against its own previous one would
// refuse every re-run.
//
// Only collisions *involving the candidate* are reported. A pair that already
// collides with each other is not this registration's fault and is startup's
// business; refusing `repo add` over it would blame the wrong command.
func ValidateNewWorktreeRoot(r *Registration, existing []store.Repo) error {
	if r == nil {
		return errors.New("repos: no registration to check for a worktree collision")
	}
	// The verification time is irrelevant here — only the path, the slug and
	// the worktree root are read, and this row is not the one that gets stored.
	// Deriving it through StoreRepo rather than assembling a store.Repo by hand
	// keeps the check reading the same fields the upsert will write.
	candidate := r.StoreRepo(time.Time{})
	dir, err := worktreeKey(candidate)
	if err != nil {
		return err
	}

	sorted := slices.Clone(existing)
	slices.SortFunc(sorted, func(a, b store.Repo) int { return strings.Compare(a.Path, b.Path) })
	for _, repo := range sorted {
		if repo.Path == candidate.Path {
			continue
		}
		other, err := worktreeKey(repo)
		if err != nil {
			// An already-stored row this check cannot key is a pre-existing
			// problem, and one the daemon refuses to start on. It is not a
			// reason to refuse the repository being added now.
			continue
		}
		if other != dir {
			continue
		}
		if repo.Path < candidate.Path {
			return collision(repo, candidate, dir)
		}
		return collision(candidate, repo, dir)
	}
	return nil
}

// worktreeKey renders the directory a repository would use for the probe
// issue. Two repositories share it exactly when they collide for every issue.
//
// [naming.WorktreePath] is Join(<worktree root>, "task-"+<issue>): the issue
// number is the last component and *nothing in the path names the repository*.
// The issue number therefore contributes an identical component on both sides
// of a comparison, so two repositories collide for some issue exactly when
// they collide for every issue — which is exactly when their cleaned, absolute
// roots are equal. That is why a check about issue numbers is answered by
// comparing roots.
//
// The key is built by calling [naming.WorktreePath] rather than by
// re-implementing its normalisation, so a trailing slash, an embedded ".." or
// a relative root cannot fool the comparison, and a later change to the
// derivation cannot silently un-couple this check from the paths it protects.
func worktreeKey(repo store.Repo) (string, error) {
	dir, err := naming.WorktreePath(repo.WorktreeRoot, collisionProbeIssue)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidWorktreeRoot, err)
	}
	return dir, nil
}

// collision builds the error for one pair, first and second already in the
// order the message should name them.
func collision(first, second store.Repo, dir string) error {
	return &WorktreeCollisionError{
		First:     CollidingRepo{Path: first.Path, Slug: first.Slug()},
		Second:    CollidingRepo{Path: second.Path, Slug: second.Slug()},
		Root:      filepath.Dir(dir),
		Issue:     collisionProbeIssue,
		Directory: dir,
	}
}

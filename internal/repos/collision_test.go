package repos_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/store"
)

// The collision tests need no clone and no database: both functions are pure
// over the rows they are handed, which is the property that lets `repo add`
// keep its store query in the command layer. Paths are literals rather than
// t.TempDir() names so that a failure message says which pair was expected.

// row builds a registered repository at path, with the given owner/name and
// worktree root.
func row(path, slug, worktreeRoot string) store.Repo {
	owner, name, _ := strings.Cut(slug, "/")
	return store.Repo{
		Path:         path,
		Forge:        store.ForgeGitHub,
		Owner:        owner,
		Name:         name,
		BaseBranch:   "main",
		WorktreeRoot: worktreeRoot,
	}
}

// registration builds the candidate `repo add` would upsert.
func registration(path, slug, worktreeRoot string) *repos.Registration {
	owner, name, _ := strings.Cut(slug, "/")
	return &repos.Registration{
		Path:         path,
		Ref:          git.RepoRef{Forge: store.ForgeGitHub, Owner: owner, Name: name},
		BaseBranch:   "main",
		WorktreeRoot: worktreeRoot,
	}
}

// TestValidateWorktreeRootsRefusesASharedRoot is the startup half of
// AC-PAR-4.8. The two roots are spelled differently on purpose: the check has
// to see through a trailing slash and a "..", because it is the *derived*
// worktree path that collides, not the stored string.
func TestValidateWorktreeRootsRefusesASharedRoot(t *testing.T) {
	t.Parallel()

	err := repos.ValidateWorktreeRoots([]store.Repo{
		row("/srv/clones/api", "acme/api", "/srv/worktrees"),
		row("/srv/clones/web", "acme/web", "/srv/tmp/../worktrees/"),
	})
	if err == nil {
		t.Fatal("two repositories under one worktree root were accepted")
	}
	if !errors.Is(err, repos.ErrWorktreeRootCollision) {
		t.Fatalf("error = %v, want one unwrapping to ErrWorktreeRootCollision", err)
	}

	var collision *repos.WorktreeCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("error = %v, want a *WorktreeCollisionError", err)
	}
	if collision.First.Path != "/srv/clones/api" || collision.Second.Path != "/srv/clones/web" {
		t.Errorf("the pair is %q and %q, want them ordered by path", collision.First.Path, collision.Second.Path)
	}
	if collision.Directory != "/srv/worktrees/task-1" {
		t.Errorf("shared directory = %q, want /srv/worktrees/task-1", collision.Directory)
	}

	// Naming both repositories is the criterion: an operator with two of them
	// cannot act on "a worktree root is shared".
	for _, want := range []string{
		"/srv/clones/api", "acme/api",
		"/srv/clones/web", "acme/web",
		"/srv/worktrees",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not name %q:\n%v", want, err)
		}
	}
}

// TestValidateWorktreeRootsAcceptsDistinctRoots is the negative: the sibling
// default gives every clone its own directory, and that is the common case.
func TestValidateWorktreeRootsAcceptsDistinctRoots(t *testing.T) {
	t.Parallel()

	for _, list := range [][]store.Repo{
		nil,
		{row("/srv/clones/api", "acme/api", "/srv/worktrees/api")},
		{
			row("/srv/clones/api", "acme/api", "/srv/worktrees/api"),
			row("/srv/clones/web", "acme/web", "/srv/worktrees/web"),
			row("/srv/clones/cli", "acme/cli", "/srv/worktrees/cli"),
		},
		// Same directory *name*, different parents: "/a/wt" and "/b/wt" are
		// two directories, and a check that compared base names would fail here.
		{
			row("/srv/clones/api", "acme/api", "/a/wt"),
			row("/srv/clones/web", "acme/web", "/b/wt"),
		},
	} {
		if err := repos.ValidateWorktreeRoots(list); err != nil {
			t.Errorf("ValidateWorktreeRoots(%d rows) = %v, want nil", len(list), err)
		}
	}
}

// TestValidateWorktreeRootsReportsEveryCollision: three repositories under one
// root is one configuration mistake, and an operator should not learn about it
// one restart at a time.
func TestValidateWorktreeRootsReportsEveryCollision(t *testing.T) {
	t.Parallel()

	err := repos.ValidateWorktreeRoots([]store.Repo{
		row("/srv/clones/web", "acme/web", "/srv/worktrees"),
		row("/srv/clones/api", "acme/api", "/srv/worktrees"),
		row("/srv/clones/cli", "acme/cli", "/srv/worktrees"),
	})
	if err == nil {
		t.Fatal("three repositories under one worktree root were accepted")
	}
	for _, want := range []string{"acme/api", "acme/web", "acme/cli"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not name %q:\n%v", want, err)
		}
	}
	if lines := strings.Count(err.Error(), "worktree path collision"); lines != 2 {
		t.Errorf("reported %d collisions, want 2 (each repository against the first claimant)", lines)
	}
}

// TestValidateWorktreeRootsRefusesAnUnusableRoot: a row this check cannot key
// is a row it is not comparing, so it fails rather than skipping. Such a row
// cannot produce a worktree path for a turn either.
func TestValidateWorktreeRootsRefusesAnUnusableRoot(t *testing.T) {
	t.Parallel()

	for _, root := range []string{"", "relative/worktrees"} {
		err := repos.ValidateWorktreeRoots([]store.Repo{row("/srv/clones/api", "acme/api", root)})
		if err == nil {
			t.Errorf("worktree root %q was accepted", root)
			continue
		}
		if !errors.Is(err, repos.ErrInvalidWorktreeRoot) {
			t.Errorf("worktree root %q failed with %v, want an invalid-worktree-root error", root, err)
		}
		if !strings.Contains(err.Error(), "/srv/clones/api") {
			t.Errorf("the message does not name the repository:\n%v", err)
		}
	}
}

// TestValidateNewWorktreeRootRefusesACollidingRegistration is the `repo add`
// half of AC-PAR-4.8: the refusal happens before the row is written, and the
// message names the repository that is already there.
func TestValidateNewWorktreeRootRefusesACollidingRegistration(t *testing.T) {
	t.Parallel()

	existing := []store.Repo{
		row("/srv/clones/api", "acme/api", "/srv/worktrees"),
		row("/srv/clones/cli", "acme/cli", "/srv/worktrees/cli"),
	}
	err := repos.ValidateNewWorktreeRoot(registration("/srv/clones/web", "acme/web", "/srv/worktrees"), existing)
	if err == nil {
		t.Fatal("a registration colliding with an existing repository was accepted")
	}
	if !errors.Is(err, repos.ErrWorktreeRootCollision) {
		t.Fatalf("error = %v, want one unwrapping to ErrWorktreeRootCollision", err)
	}
	for _, want := range []string{"/srv/clones/api", "acme/api", "/srv/clones/web", "acme/web"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not name %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "acme/cli") {
		t.Errorf("the message names a repository that does not collide:\n%v", err)
	}
}

// TestValidateNewWorktreeRootExcludesTheRowItReplaces guards the false
// positive that would matter most: `repo add` on an already-registered path
// updates that row (AC-1.8), so a registration must not collide with its own
// previous one. Getting this wrong breaks every re-run of `repo add`.
func TestValidateNewWorktreeRootExcludesTheRowItReplaces(t *testing.T) {
	t.Parallel()

	existing := []store.Repo{
		row("/srv/clones/api", "acme/api", "/srv/worktrees"),
		row("/srv/clones/web", "acme/web", "/srv/worktrees/web"),
	}
	for _, candidate := range []*repos.Registration{
		// The same registration again.
		registration("/srv/clones/api", "acme/api", "/srv/worktrees"),
		// The same path, spelling the root differently — still the same row.
		registration("/srv/clones/api", "acme/api", "/srv/worktrees/"),
		// The same path, renamed on the forge, which is a rename rather than a
		// second repository.
		registration("/srv/clones/api", "acme/api-v2", "/srv/worktrees"),
	} {
		if err := repos.ValidateNewWorktreeRoot(candidate, existing); err != nil {
			t.Errorf("re-registering %s (%s) was refused: %v", candidate.Path, candidate.Ref.Slug(), err)
		}
	}
}

// TestValidateNewWorktreeRootAcceptsADistinctRoot covers the ordinary case,
// including a table that already contains a collision between two *other*
// rows: that is startup's business, and refusing this registration over it
// would blame the wrong command.
func TestValidateNewWorktreeRootAcceptsADistinctRoot(t *testing.T) {
	t.Parallel()

	existing := []store.Repo{
		row("/srv/clones/api", "acme/api", "/srv/worktrees"),
		row("/srv/clones/web", "acme/web", "/srv/worktrees"),
	}
	candidate := registration("/srv/clones/cli", "acme/cli", "/srv/worktrees/cli")
	if err := repos.ValidateNewWorktreeRoot(candidate, existing); err != nil {
		t.Errorf("ValidateNewWorktreeRoot = %v, want nil", err)
	}
	if err := repos.ValidateNewWorktreeRoot(candidate, nil); err != nil {
		t.Errorf("ValidateNewWorktreeRoot against an empty table = %v, want nil", err)
	}
}

// TestValidateNewWorktreeRootRefusesAnUnusableRoot: the candidate's own root
// has to key, or the check has not been run on it.
func TestValidateNewWorktreeRootRefusesAnUnusableRoot(t *testing.T) {
	t.Parallel()

	err := repos.ValidateNewWorktreeRoot(registration("/srv/clones/api", "acme/api", "worktrees"), nil)
	if !errors.Is(err, repos.ErrInvalidWorktreeRoot) {
		t.Errorf("error = %v, want an invalid-worktree-root error", err)
	}
	if err := repos.ValidateNewWorktreeRoot(nil, nil); err == nil {
		t.Error("a nil registration was accepted")
	}
}

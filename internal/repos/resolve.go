package repos

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// ResolveIdentifier turns the operator-typed <repo-identifier> argument of
// `team add` into exactly one registered repository.
//
// The shape of identifier picks the resolution path, and only one path is
// ever tried — this never falls back from one to the other:
//
//   - identifier contains "/": treated as an exact owner/name slug and looked
//     up with [store.DB.GetRepoBySlug]. A substring match is never attempted
//     for this shape (US-19 AC-1).
//   - identifier contains no "/": matched as a case-insensitive substring of
//     Name across every repository [store.DB.ListRepos] returns.
//
// Zero matches is a [*RepoNotFoundError] naming identifier exactly as typed
// (AC-4). Exactly one match resolves silently (AC-2). More than one match is
// a [*AmbiguousRepoError] listing every candidate's full owner/name — this
// never guesses which one was meant (AC-3).
func ResolveIdentifier(ctx context.Context, db *store.DB, identifier string) (store.Repo, error) {
	if strings.Contains(identifier, "/") {
		return resolveBySlug(ctx, db, identifier)
	}
	return resolveByNameSubstring(ctx, db, identifier)
}

// resolveBySlug handles the "contains /" path: an exact forge:owner/name
// lookup. litefactory speaks exactly one forge at M0 ([store.ForgeGitHub]), so
// that is the only forge tried.
func resolveBySlug(ctx context.Context, db *store.DB, identifier string) (store.Repo, error) {
	owner, name, _ := strings.Cut(identifier, "/")
	repo, err := db.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return store.Repo{}, &RepoNotFoundError{Identifier: identifier}
	case err != nil:
		return store.Repo{}, fmt.Errorf("resolve repo %q: %w", identifier, err)
	}
	return repo, nil
}

// resolveByNameSubstring handles the "no /" path: a case-insensitive
// substring match against Name across every registered repository.
func resolveByNameSubstring(ctx context.Context, db *store.DB, identifier string) (store.Repo, error) {
	all, err := db.ListRepos(ctx)
	if err != nil {
		return store.Repo{}, fmt.Errorf("resolve repo %q: %w", identifier, err)
	}

	needle := strings.ToLower(identifier)
	var matches []store.Repo
	for _, repo := range all {
		if strings.Contains(strings.ToLower(repo.Name), needle) {
			matches = append(matches, repo)
		}
	}

	switch len(matches) {
	case 0:
		return store.Repo{}, &RepoNotFoundError{Identifier: identifier}
	case 1:
		return matches[0], nil
	default:
		return store.Repo{}, &AmbiguousRepoError{Identifier: identifier, Candidates: matches}
	}
}

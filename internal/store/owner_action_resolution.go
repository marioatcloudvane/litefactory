package store

import (
	"context"
	"errors"
	"time"
)

type ownerActionResolutionKey struct{}
type ownerActionResolution struct {
	principal OwnerActionPrincipal
	scope     string
	clock     func() time.Time
}

// WithOwnerActionResolution carries authenticated action authority to target
// resolution, before any repository content is read or disclosed.
func WithOwnerActionResolution(ctx context.Context, principal OwnerActionPrincipal, scope string, clock func() time.Time) context.Context {
	return context.WithValue(ctx, ownerActionResolutionKey{}, ownerActionResolution{principal: principal, scope: scope, clock: clock})
}

// CheckOwnerActionResolution checks current consent and repository coverage for
// action target reads. Legacy callers retain their existing authorization path.
func (db *DB) CheckOwnerActionResolution(ctx context.Context, repositoryID int64, repositoryRef string) error {
	authority, ok := ctx.Value(ownerActionResolutionKey{}).(ownerActionResolution)
	if !ok {
		return nil
	}
	if authority.clock == nil || authority.scope == "" {
		return errors.New("owner action resolution authority unavailable")
	}
	return db.CheckOwnerActionAuthority(ctx, authority.principal, OwnerAction{Principal: authority.principal, Scope: authority.scope, RepositoryID: repositoryID, RepositoryRef: repositoryRef}, authority.clock())
}

// ListOwnerActionResolutionRepositories limits project inference to repositories
// the current action consent can read, including an authorization check when
// no repositories are registered.
func (db *DB) ListOwnerActionResolutionRepositories(ctx context.Context) ([]Repo, error) {
	if _, ok := ctx.Value(ownerActionResolutionKey{}).(ownerActionResolution); !ok {
		return db.ListRepos(ctx)
	}
	if err := db.CheckOwnerActionResolution(ctx, 0, ""); err != nil {
		return nil, err
	}
	repositories, err := db.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make([]Repo, 0, len(repositories))
	for _, repo := range repositories {
		err = db.CheckOwnerActionResolution(ctx, repo.ID, repo.Forge+":"+repo.Slug())
		if errors.Is(err, ErrConnectorScope) {
			continue
		}
		if err != nil {
			return nil, err
		}
		allowed = append(allowed, repo)
	}
	return allowed, nil
}

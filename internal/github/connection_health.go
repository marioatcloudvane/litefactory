package github

import (
	"context"
	"fmt"
	gogithub "github.com/google/go-github/v90/github"
)

// RepositoryAccess is read-only evidence, not a guarantee that an effect succeeds.
type RepositoryAccess struct {
	ID            int64
	FullName      string
	IssuesEnabled bool
}

// InspectRepository reads destination identity and available permission evidence.
func (c *Client) InspectRepository(ctx context.Context, repo Repo) (RepositoryAccess, error) {
	if err := repo.Validate(); err != nil {
		return RepositoryAccess{}, err
	}
	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Repository, *gogithub.Response, error) {
		return c.api.Repositories.Get(ctx, repo.Owner, repo.Name)
	})
	if err != nil {
		return RepositoryAccess{}, fmt.Errorf("github: inspect repository: %w", err)
	}
	if payload == nil || payload.GetID() <= 0 {
		return RepositoryAccess{}, fmt.Errorf("github: inspect repository: %w", ErrUnexpectedStatus)
	}
	return RepositoryAccess{ID: payload.GetID(), FullName: payload.GetFullName(), IssuesEnabled: payload.GetHasIssues()}, nil
}

package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// ReadOpenIssues supplies ask's bounded, newest-updated project context. It
// reads one remote page only, names further pages as omissions, and filters PRs.
func (c *Client) ReadOpenIssues(ctx context.Context, repo Repo, limit int) ([]Issue, bool, error) {
	if err := repo.Validate(); err != nil {
		return nil, false, err
	}
	if limit < 1 || limit > 50 {
		return nil, false, fmt.Errorf("github: invalid chat issue limit: %w", ErrInvalidArgument)
	}
	opts := &gogithub.IssueListByRepoOptions{State: StateOpen, Sort: "updated", Direction: "desc", ListOptions: gogithub.ListOptions{Page: 1, PerPage: limit + 1}}
	items, response, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.ListByRepo(ctx, repo.Owner, repo.Name, opts)
	})
	if err != nil {
		return nil, false, err
	}
	if len(items) > limit+1 {
		return nil, false, fmt.Errorf("github: oversized chat issue page")
	}
	var issues []Issue
	for _, item := range items {
		if item != nil && !item.IsPullRequest() && item.GetState() == StateOpen {
			issues = append(issues, convertIssue(item))
		}
	}
	more := len(issues) > limit || (response != nil && response.NextPage > 1)
	return issues[:min(limit, len(issues))], more, nil
}

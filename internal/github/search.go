package github

import (
	"context"
	"fmt"
	"strings"

	gogithub "github.com/google/go-github/v90/github"
)

// SearchIssues returns at most limit issues of repo, open and closed, whose
// title or body match text, best match first. It is a lookup for an analyst
// comparing a finding with existing issues, never discovery: the search index
// is eventually consistent and has its own small rate limit (see ListIssues).
func (c *Client) SearchIssues(ctx context.Context, repo Repo, text string, limit int) ([]Issue, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || len(text) > 200 || limit < 1 || limit > 50 {
		return nil, fmt.Errorf("github: search issues: %w", ErrInvalidArgument)
	}
	query := fmt.Sprintf("repo:%s/%s is:issue %s", repo.Owner, repo.Name, text)
	opts := &gogithub.SearchOptions{ListOptions: gogithub.ListOptions{Page: 1, PerPage: limit}}
	result, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.IssuesSearchResult, *gogithub.Response, error) {
		return c.api.Search.Issues(ctx, query, opts)
	})
	if err != nil {
		return nil, fmt.Errorf("github: search issues in %s: %w", repo, err)
	}
	issues := []Issue{}
	if result == nil {
		return issues, nil
	}
	for _, payload := range result.Issues {
		if payload != nil && !payload.IsPullRequest() && len(issues) < limit {
			issues = append(issues, convertIssue(payload))
		}
	}
	return issues, nil
}

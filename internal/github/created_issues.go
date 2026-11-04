package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// ListCreatedIssuesPage reads one page of the repository's issues opened by
// creator, open and closed, newest first. It reads the issue listing directly,
// not the search index; pagination is never followed internally.
func (c *Client) ListCreatedIssuesPage(ctx context.Context, repo Repo, creator string, page, perPage int) (IssuePage, error) {
	out := IssuePage{Items: []Issue{}}
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if creator == "" || page < 1 || perPage < 1 || perPage > 100 {
		return out, fmt.Errorf("github: invalid created issue page: %w", ErrInvalidArgument)
	}
	opts := &gogithub.IssueListByRepoOptions{Creator: creator, State: "all", Sort: "created", Direction: "desc"}
	opts.ListOptions = gogithub.ListOptions{Page: page, PerPage: perPage}
	payloads, response, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.ListByRepo(ctx, repo.Owner, repo.Name, opts)
	})
	if err != nil {
		return out, fmt.Errorf("github: list created issue page in %s: %w", repo, err)
	}
	if len(payloads) > perPage {
		return out, fmt.Errorf("github: created issue page exceeds requested bound")
	}
	for _, payload := range payloads {
		if payload != nil && !payload.IsPullRequest() {
			out.Items = append(out.Items, convertIssue(payload))
		}
	}
	if response != nil && response.NextPage > page {
		out.NextPage = response.NextPage
	}
	return out, nil
}

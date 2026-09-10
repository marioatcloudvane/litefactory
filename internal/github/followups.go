package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// ListFollowupIssues includes closed issues so uncertain publication can be
// reconciled even when a human closed the new issue before recovery.
func (c *Client) ListFollowupIssues(ctx context.Context, repo Repo) ([]Issue, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	payloads, _, err := fetchPages(ctx, c, func(ctx context.Context, page int) ([]*gogithub.Issue, *gogithub.Response, error) {
		opts := gogithub.IssueListByRepoOptions{State: "all", Sort: "created", Direction: "asc"}
		opts.ListOptions.Page = page
		opts.ListOptions.PerPage = perPage
		return c.api.Issues.ListByRepo(ctx, repo.Owner, repo.Name, &opts)
	})
	if err != nil {
		return nil, fmt.Errorf("github: reconcile followups for %s: %w", repo, err)
	}
	issues := make([]Issue, 0, len(payloads))
	for _, p := range payloads {
		if p != nil && !p.IsPullRequest() {
			issues = append(issues, convertIssue(p))
		}
	}
	return issues, nil
}

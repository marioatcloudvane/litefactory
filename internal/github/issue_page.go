package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// IssuePage represents exactly one remote page. PR filtering may leave Items
// empty while NextPage still points to more results; callers must not infer a
// complete catalog from the number of returned issues.
type IssuePage struct {
	Items    []Issue
	NextPage int
}

// ListIssuesPage reads a single bounded page without changing daemon polling's
// accumulating ListIssues contract. Pagination is never followed internally.
func (c *Client) ListIssuesPage(ctx context.Context, repo Repo, state string, page, perPage int) (IssuePage, error) {
	out := IssuePage{Items: []Issue{}}
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if (state != StateOpen && state != "closed") || page < 1 || perPage < 1 || perPage > 100 {
		return out, fmt.Errorf("github: invalid issue page: %w", ErrInvalidArgument)
	}
	opts := &gogithub.IssueListByRepoOptions{State: state, Sort: "created", Direction: "asc"}
	opts.ListOptions = gogithub.ListOptions{Page: page, PerPage: perPage}
	payloads, response, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.ListByRepo(ctx, repo.Owner, repo.Name, opts)
	})
	if err != nil {
		return out, fmt.Errorf("github: list issue page in %s: %w", repo, err)
	}
	if len(payloads) > perPage {
		return out, fmt.Errorf("github: issue page exceeds requested bound")
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

package github

import (
	"context"
	"fmt"
	gogithub "github.com/google/go-github/v90/github"
)

// CommentPage is one bounded target-local observation, never a complete-history
// claim. An empty page cannot establish that an uncertain POST did not happen.
type CommentPage struct {
	Comments []Comment
	NextPage int
}

// ListCommentsPage reads one bounded page for target-local comment observation.
func (c *Client) ListCommentsPage(ctx context.Context, repo Repo, issue, page, limit int) (CommentPage, error) {
	var out CommentPage
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if err := checkIssueNumber(issue); err != nil {
		return out, err
	}
	if page < 1 || limit < 1 || limit > 100 {
		return out, fmt.Errorf("github: invalid comment page")
	}
	payloads, response, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.IssueComment, *gogithub.Response, error) {
		return c.api.Issues.ListComments(ctx, repo.Owner, repo.Name, issue, &gogithub.IssueListCommentsOptions{ListOptions: gogithub.ListOptions{Page: page, PerPage: limit}})
	})
	if err != nil {
		return out, fmt.Errorf("github: observe comments: %w", err)
	}
	if len(payloads) > limit {
		return out, fmt.Errorf("github: comment page exceeds bound")
	}
	for _, p := range payloads {
		if p != nil {
			out.Comments = append(out.Comments, convertComment(p))
		}
	}
	if response != nil {
		out.NextPage = response.NextPage
	}
	return out, nil
}

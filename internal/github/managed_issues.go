package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/redact"
)

// GetParentIssue checks the current direct relationship; a marker is not
// authority to move an issue from a different parent.
func (c *Client) GetParentIssue(ctx context.Context, repo Repo, child int) (Issue, error) {
	if err := repo.Validate(); err != nil {
		return Issue{}, err
	}
	if err := checkIssueNumber(child); err != nil {
		return Issue{}, err
	}
	issue, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		return c.api.SubIssue.GetParentIssue(ctx, repo.Owner, repo.Name, int64(child))
	})
	if err != nil {
		return Issue{}, fmt.Errorf("github: get parent of %s#%d: %w", repo, child, err)
	}
	if issue == nil || issue.GetID() < 1 || issue.GetNumber() < 1 || issue.IsPullRequest() {
		return Issue{}, fmt.Errorf("github: invalid parent issue response")
	}
	return convertIssue(issue), nil
}

// UpdateIssue replaces the complete editable specification. GitHub provides no
// atomic compare-and-swap here: callers compare a fresh base, then verify the
// response with a read and report the remaining remote-edit race honestly.
func (c *Client) UpdateIssue(ctx context.Context, repo Repo, number int, title, body redact.Body) (Issue, error) {
	if err := repo.Validate(); err != nil {
		return Issue{}, err
	}
	if err := checkIssueNumber(number); err != nil {
		return Issue{}, err
	}
	if err := checkIssueTitle(title); err != nil {
		return Issue{}, err
	}
	if err := checkIssueBody(body); err != nil {
		return Issue{}, err
	}
	if title.Truncated() || body.Truncated() {
		return Issue{}, fmt.Errorf("%w: complete story cannot be truncated", ErrInvalidArgument)
	}
	t, b := title.String(), body.String()
	issue, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.Update(ctx, repo.Owner, repo.Name, number, gogithub.UpdateIssueRequest{Title: &t, Body: &b})
	})
	if err != nil {
		return Issue{}, fmt.Errorf("github: update issue %s#%d: %w", repo, number, err)
	}
	if issue == nil || issue.GetNumber() != number || issue.GetID() < 1 || issue.IsPullRequest() {
		return Issue{}, fmt.Errorf("github: invalid updated issue response")
	}
	return convertIssue(issue), nil
}

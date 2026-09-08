package github

import (
	"context"
	"fmt"
	gogithub "github.com/google/go-github/v90/github"
)

// SetStoryState changes only state on a numerically verified direct sub-issue.
// It does not infer verification from a human's close or modify story content.
func (c *Client) SetStoryState(ctx context.Context, repo Repo, parentNumber int, parentID int64, number int, issueID int64, completed bool) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if err := checkIssueNumber(number); err != nil {
		return err
	}
	if parentID < 1 || issueID < 1 {
		return fmt.Errorf("%w: missing managed identity", ErrInvalidArgument)
	}
	child, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	parent, err := c.GetParentIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	if child.ID != issueID || parent.ID != parentID || parent.Number != parentNumber {
		return fmt.Errorf("github: managed story relationship changed")
	}
	state, reason := "open", "reopened"
	if completed {
		state, reason = "closed", "completed"
	}
	issue, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.Update(ctx, repo.Owner, repo.Name, number, gogithub.UpdateIssueRequest{State: &state, StateReason: &reason})
	})
	if err != nil {
		return fmt.Errorf("github: set managed story state: %w", err)
	}
	if issue == nil || issue.GetID() != issueID || issue.GetNumber() != number || issue.GetState() != state {
		return fmt.Errorf("github: invalid story state response")
	}
	return nil
}

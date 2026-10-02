package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// RetirePlanStory closes only a verified direct child, using not-planned rather
// than completion semantics. Exact owner/candidate authority belongs to turn.
func (c *Client) RetirePlanStory(ctx context.Context, repo Repo, parentNumber int, parentID int64, number int, issueID int64) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if parentNumber < 1 || parentID < 1 || number < 1 || issueID < 1 {
		return fmt.Errorf("%w: missing Plan identity", ErrInvalidArgument)
	}
	child, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	parent, err := c.GetParentIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	if child.ID != issueID || parent.ID != parentID || parent.Number != parentNumber || child.State != StateOpen {
		return fmt.Errorf("github: Plan story relationship or state changed")
	}
	state, reason := "closed", "not_planned"
	issue, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.Update(ctx, repo.Owner, repo.Name, number, gogithub.UpdateIssueRequest{State: &state, StateReason: &reason})
	})
	if err != nil {
		return fmt.Errorf("github: retire Plan story: %w", err)
	}
	if issue == nil || issue.GetID() != issueID || issue.GetNumber() != number || issue.GetState() != state || issue.GetStateReason() != reason {
		return fmt.Errorf("github: invalid Plan retirement response")
	}
	return nil
}

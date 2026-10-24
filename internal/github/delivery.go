package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	gogithub "github.com/google/go-github/v90/github"
)

// DeliveryPR carries remote identities and independently observed merge facts.
type DeliveryPR struct {
	ID, CreatorID                                                          int64
	Number                                                                 int
	Title, Body, Head, Branch, Target, HeadRepo, BaseRepo, State, MergeSHA string
	Merged                                                                 bool
	MergedAt                                                               time.Time
}

func convertDeliveryPR(p *gogithub.PullRequest) DeliveryPR {
	if p == nil {
		return DeliveryPR{}
	}
	return DeliveryPR{ID: p.GetID(), CreatorID: p.GetUser().GetID(), Number: p.GetNumber(), Title: p.GetTitle(), Body: p.GetBody(), Head: p.GetHead().GetSHA(), Branch: p.GetHead().GetRef(), Target: p.GetBase().GetRef(), HeadRepo: p.GetHead().GetRepo().GetFullName(), BaseRepo: p.GetBase().GetRepo().GetFullName(), State: p.GetState(), MergeSHA: p.GetMergeCommitSHA(), Merged: p.GetMerged() || !p.GetMergedAt().IsZero(), MergedAt: p.GetMergedAt().Time}
}
func validDeliverySHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

// GetDeliveryHead reads and validates the remote task branch head.
func (c *Client) GetDeliveryHead(ctx context.Context, repo Repo, branch string) (string, error) {
	if err := repo.Validate(); err != nil {
		return "", err
	}
	if branch == "" || strings.ContainsAny(branch, "\r\n\x00") {
		return "", fmt.Errorf("invalid delivery branch")
	}
	b, resp, err := do(ctx, c, func(ctx context.Context) (*gogithub.Branch, *gogithub.Response, error) {
		return c.api.Repositories.GetBranch(ctx, repo.Owner, repo.Name, branch, 0)
	})
	if err != nil {
		// GetBranch reports a missing branch as a plain "unexpected status
		// code" error, so the 404 must be typed here for callers to see it.
		var api *APIError
		if !errors.As(err, &api) && resp != nil && resp.Response != nil && resp.StatusCode == http.StatusNotFound {
			return "", c.sanitizeFailure(ctx, &APIError{Method: requestMethod(resp.Response), URL: requestURL(resp.Response), StatusCode: http.StatusNotFound, Message: err.Error(), RequestID: resp.Header.Get("X-GitHub-Request-Id"), Err: ErrNotFound})
		}
		return "", err
	}
	if b == nil || b.GetName() != branch || !validDeliverySHA(b.GetCommit().GetSHA()) {
		return "", fmt.Errorf("invalid remote branch identity")
	}
	return b.GetCommit().GetSHA(), nil
}

// ListDeliveryPRs lists all matching branch PRs, including merged and closed PRs.
func (c *Client) ListDeliveryPRs(ctx context.Context, repo Repo, branch string) ([]DeliveryPR, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if branch == "" {
		return nil, fmt.Errorf("empty delivery branch")
	}
	raw, _, err := fetchPages(ctx, c, func(ctx context.Context, page int) ([]*gogithub.PullRequest, *gogithub.Response, error) {
		o := gogithub.PullRequestListOptions{State: "all", Head: repo.Owner + ":" + branch}
		o.Page = page
		o.PerPage = perPage
		return c.api.PullRequests.List(ctx, repo.Owner, repo.Name, &o)
	})
	if err != nil {
		return nil, err
	}
	var out []DeliveryPR
	for _, p := range raw {
		if p != nil {
			out = append(out, convertDeliveryPR(p))
		}
	}
	return out, nil
}

// GetDeliveryPR independently observes the identified remote PR and merge facts.
func (c *Client) GetDeliveryPR(ctx context.Context, repo Repo, number int) (DeliveryPR, error) {
	if err := repo.Validate(); err != nil {
		return DeliveryPR{}, err
	}
	if err := checkIssueNumber(number); err != nil {
		return DeliveryPR{}, err
	}
	p, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.PullRequest, *gogithub.Response, error) {
		return c.api.PullRequests.Get(ctx, repo.Owner, repo.Name, number)
	})
	if err != nil {
		return DeliveryPR{}, err
	}
	v := convertDeliveryPR(p)
	if v.ID < 1 || v.Number != number {
		return v, fmt.Errorf("invalid delivery PR identity")
	}
	return v, nil
}

// CreateDeliveryPR creates a redacted task-branch PR into the repository's base branch.
func (c *Client) CreateDeliveryPR(ctx context.Context, repo Repo, branch, target string, title, body redact.Body) (DeliveryPR, error) {
	if err := repo.Validate(); err != nil {
		return DeliveryPR{}, err
	}
	if branch == "" || target == "" || branch == target {
		return DeliveryPR{}, fmt.Errorf("invalid delivery branches")
	}
	if err := checkIssueTitle(title); err != nil {
		return DeliveryPR{}, err
	}
	if err := checkIssueBody(body); err != nil {
		return DeliveryPR{}, err
	}
	t, b := title.String(), body.String()
	p, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.PullRequest, *gogithub.Response, error) {
		return c.api.PullRequests.Create(ctx, repo.Owner, repo.Name, gogithub.CreatePullRequest{Title: &t, Body: &b, Head: branch, Base: target})
	})
	if err != nil {
		return DeliveryPR{}, err
	}
	v := convertDeliveryPR(p)
	if v.ID < 1 || v.Number < 1 {
		return v, fmt.Errorf("invalid created PR identity")
	}
	return v, nil
}

// MergeDeliveryPR uses GitHub's atomic expected-head guard. Its response is not
// integration proof: callers must independently GetDeliveryPR before closure.
func (c *Client) MergeDeliveryPR(ctx context.Context, repo Repo, number int, head string) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if err := checkIssueNumber(number); err != nil {
		return err
	}
	if !validDeliverySHA(head) {
		return fmt.Errorf("invalid delivery merge head")
	}
	r, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.PullRequestMergeResult, *gogithub.Response, error) {
		return c.api.PullRequests.Merge(ctx, repo.Owner, repo.Name, number, "", &gogithub.PullRequestOptions{SHA: head, MergeMethod: "merge"})
	})
	if err != nil {
		return err
	}
	if r == nil || !r.GetMerged() || !validDeliverySHA(r.GetSHA()) {
		return fmt.Errorf("delivery merge was not successful")
	}
	return nil
}

// CloseDeliveredParent verifies numeric identity before the ordered close effect.
func (c *Client) CloseDeliveredParent(ctx context.Context, repo Repo, number int, id int64) error {
	issue, err := c.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	if id < 1 || issue.ID != id {
		return fmt.Errorf("delivery parent identity mismatch")
	}
	if issue.State == "closed" {
		return nil
	}
	state, reason := "closed", "completed"
	_, _, err = do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.Update(ctx, repo.Owner, repo.Name, number, gogithub.UpdateIssueRequest{State: &state, StateReason: &reason})
	})
	return err
}

package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// DeliveryCommit is independent immutable integration evidence, retaining parent order.
type DeliveryCommit struct {
	SHA     string
	Parents []string
}

// GetDeliveryCommit validates the requested object and every ordered parent.
func (c *Client) GetDeliveryCommit(ctx context.Context, repo Repo, sha string) (DeliveryCommit, error) {
	if err := repo.Validate(); err != nil {
		return DeliveryCommit{}, err
	}
	if !validDeliverySHA(sha) {
		return DeliveryCommit{}, fmt.Errorf("invalid delivery commit")
	}
	p, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Commit, *gogithub.Response, error) {
		return c.api.Git.GetCommit(ctx, repo.Owner, repo.Name, sha)
	})
	if err != nil {
		return DeliveryCommit{}, err
	}
	if p == nil || p.GetSHA() != sha || len(p.Parents) > 64 {
		return DeliveryCommit{}, fmt.Errorf("invalid delivery commit identity")
	}
	out := DeliveryCommit{SHA: sha}
	for _, parent := range p.Parents {
		if parent == nil || !validDeliverySHA(parent.GetSHA()) {
			return DeliveryCommit{}, fmt.Errorf("invalid delivery commit parent")
		}
		out.Parents = append(out.Parents, parent.GetSHA())
	}
	return out, nil
}

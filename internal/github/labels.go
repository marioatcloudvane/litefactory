package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// ListLabelNames reads the name of every label in the repository across all
// pages. A failed page or the page bound is an error, never a partial list.
func (c *Client) ListLabelNames(ctx context.Context, repo Repo) ([]string, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	payloads, _, err := fetchPages(ctx, c, func(ctx context.Context, page int) ([]*gogithub.Label, *gogithub.Response, error) {
		return c.api.Issues.ListLabels(ctx, repo.Owner, repo.Name, &gogithub.ListOptions{Page: page, PerPage: perPage})
	})
	if err != nil {
		return nil, fmt.Errorf("github: list labels in %s: %w", repo, err)
	}
	names := make([]string, 0, len(payloads))
	for _, p := range payloads {
		if p != nil && p.GetName() != "" {
			names = append(names, p.GetName())
		}
	}
	return names, nil
}

// CreateLabel creates one label. There is deliberately no update or delete.
func (c *Client) CreateLabel(ctx context.Context, repo Repo, name, color, description string) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("github: create label: %w", ErrInvalidArgument)
	}
	_, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Label, *gogithub.Response, error) {
		return c.api.Issues.CreateLabel(ctx, repo.Owner, repo.Name, gogithub.CreateIssueLabelRequest{Name: name, Color: &color, Description: &description})
	})
	if err != nil {
		return fmt.Errorf("github: create label in %s: %w", repo, err)
	}
	return nil
}

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	gogithub "github.com/google/go-github/v90/github"
)

// RepoLabelNodeID reads one repository label by its exact name and returns its
// GraphQL node ID. A missing label, or one whose name differs from name in any
// way GitHub's case-insensitive lookup tolerates, reports found=false with no
// error. It never creates a label.
func (c *Client) RepoLabelNodeID(ctx context.Context, repo Repo, name string) (nodeID string, found bool, err error) {
	if err := repo.Validate(); err != nil {
		return "", false, err
	}
	if name == "" {
		return "", false, fmt.Errorf("github: read label: %w: name required", ErrInvalidArgument)
	}
	label, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Label, *gogithub.Response, error) {
		return c.api.Issues.GetLabel(ctx, repo.Owner, repo.Name, url.PathEscape(name))
	})
	if errors.Is(err, ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("github: read label in %s: %w", repo, err)
	}
	if label == nil || label.GetName() != name {
		return "", false, nil
	}
	if label.GetNodeID() == "" {
		return "", false, fmt.Errorf("github: read label in %s: %w: response carried no node id", repo, ErrUnexpectedStatus)
	}
	return label.GetNodeID(), true, nil
}

type addLabelsEnvelope struct {
	Data struct {
		AddLabelsToLabelable *struct {
			ClientMutationID *string `json:"clientMutationId"`
		} `json:"addLabelsToLabelable"`
	} `json:"data"`
	Errors []struct {
		Type       string `json:"type"`
		Message    string `json:"message"`
		Extensions struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

// AddLabelsByID adds existing labels, named only by their node IDs, to an
// issue or pull request through GraphQL addLabelsToLabelable. Labels already
// present stay. The mutation's name-based field is never sent, so an unknown
// label is refused rather than created.
func (c *Client) AddLabelsByID(ctx context.Context, labelableNodeID string, labelIDs []string) error {
	if labelableNodeID == "" || len(labelIDs) == 0 {
		return fmt.Errorf("github: add labels by id: %w: labelable and label ids required", ErrInvalidArgument)
	}
	for _, id := range labelIDs {
		if id == "" {
			return fmt.Errorf("github: add labels by id: %w: empty label id", ErrInvalidArgument)
		}
	}
	_, _, err := do(ctx, c, func(ctx context.Context) (struct{}, *gogithub.Response, error) {
		endpoint, err := c.parentGraphQLEndpoint()
		if err != nil {
			return struct{}{}, nil, err
		}
		body := struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}{
			Query:     `mutation($labelable:ID!,$ids:[ID!]!){addLabelsToLabelable(input:{labelableId:$labelable,labelIds:$ids}){clientMutationId}}`,
			Variables: map[string]any{"labelable": labelableNodeID, "ids": labelIDs},
		}
		req, err := c.api.NewRequest(ctx, http.MethodPost, endpoint, body)
		if err != nil {
			return struct{}{}, nil, err
		}
		var envelope addLabelsEnvelope
		response, err := c.api.Do(req, &envelope)
		if err != nil {
			return struct{}{}, response, err
		}
		if len(envelope.Errors) > 0 {
			for _, failure := range envelope.Errors {
				if failure.Type == "RATE_LIMITED" || failure.Extensions.Type == "RATE_LIMITED" || failure.Extensions.Code == "RATE_LIMITED" {
					delay, reset := retryDelay(response.Header, c.clock())
					return struct{}{}, response, &RateLimitError{
						APIError: APIError{Method: requestMethod(response.Response), URL: requestURL(response.Response),
							StatusCode: response.StatusCode, Message: "GraphQL add labels rate limited", Err: ErrRateLimited},
						RetryAfter: delay, Reset: reset, Limit: headerInt(response.Header, "X-RateLimit-Limit"),
						Remaining: headerInt(response.Header, "X-RateLimit-Remaining"), Resource: "graphql",
					}
				}
			}
			return struct{}{}, response, errors.New("github: add labels by id: GraphQL errors refused the mutation")
		}
		if envelope.Data.AddLabelsToLabelable == nil {
			return struct{}{}, response, errors.New("github: add labels by id: response carried no mutation result")
		}
		return struct{}{}, response, nil
	})
	return err
}

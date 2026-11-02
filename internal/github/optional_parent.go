package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// ParentIssueObservation identifies a direct parent observed in the configured
// API instance. Repository identity comes from the relationship observation,
// rather than from issue text or the child's repository.
type ParentIssueObservation struct {
	ID     int64
	Number int
	Repo   Repo
}

type parentIssueNode struct {
	DatabaseID int64 `json:"databaseId"`
	Number     int   `json:"number"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Parent json.RawMessage `json:"parent"`
}

type parentIssueEnvelope struct {
	Data struct {
		Repository *struct {
			Issue *parentIssueNode `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Type       string `json:"type"`
		Extensions struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

// ObserveParentIssue returns nil only when an error-free response binds the
// expected immutable child to an explicitly null parent. REST 404 responses do
// not establish absence, and the mutation-oriented GetParentIssue is unchanged.
func (c *Client) ObserveParentIssue(ctx context.Context, repo Repo, number int, expectedChildID int64) (*ParentIssueObservation, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if number < 1 || expectedChildID < 1 {
		return nil, fmt.Errorf("github: observe parent: %w: child number and immutable ID required", ErrInvalidArgument)
	}
	parent, _, err := do(ctx, c, func(ctx context.Context) (*ParentIssueObservation, *gogithub.Response, error) {
		endpoint, err := c.parentGraphQLEndpoint()
		if err != nil {
			return nil, nil, err
		}
		body := struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}{
			Query:     `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){databaseId number repository{nameWithOwner} parent{databaseId number repository{nameWithOwner}}}}}`,
			Variables: map[string]any{"owner": repo.Owner, "name": repo.Name, "number": number},
		}
		req, err := c.api.NewRequest(ctx, http.MethodPost, endpoint, body)
		if err != nil {
			return nil, nil, err
		}
		var envelope parentIssueEnvelope
		response, err := c.api.Do(req, &envelope)
		if err != nil {
			return nil, response, err
		}
		if len(envelope.Errors) > 0 {
			return nil, response, c.parentGraphQLError(response, envelope)
		}
		if envelope.Data.Repository == nil || envelope.Data.Repository.Issue == nil {
			return nil, response, errors.New("github: observe parent: child is missing or inaccessible")
		}
		child := envelope.Data.Repository.Issue
		if child.DatabaseID != expectedChildID || child.Number != number || !strings.EqualFold(child.Repository.NameWithOwner, repo.String()) {
			return nil, response, errors.New("github: observe parent: child immutable identity or repository differs")
		}
		if len(child.Parent) == 0 {
			return nil, response, errors.New("github: observe parent: response has no parent observation")
		}
		if bytes.Equal(bytes.TrimSpace(child.Parent), []byte("null")) {
			return nil, response, nil
		}
		var observed parentIssueNode
		if err := json.Unmarshal(child.Parent, &observed); err != nil {
			return nil, response, fmt.Errorf("github: observe parent: decode parent: %w", err)
		}
		parts := strings.Split(observed.Repository.NameWithOwner, "/")
		if observed.DatabaseID < 1 || observed.DatabaseID == expectedChildID || observed.Number < 1 || len(parts) != 2 {
			return nil, response, errors.New("github: observe parent: parent identity is missing or invalid")
		}
		parentRepo := Repo{Owner: parts[0], Name: parts[1]}
		if err := parentRepo.Validate(); err != nil {
			return nil, response, fmt.Errorf("github: observe parent: parent repository: %w", err)
		}
		return &ParentIssueObservation{ID: observed.DatabaseID, Number: observed.Number, Repo: parentRepo}, response, nil
	})
	return parent, err
}

func (c *Client) parentGraphQLEndpoint() (string, error) {
	endpoint := *c.base
	switch path := strings.TrimRight(endpoint.Path, "/"); {
	case path == "":
		endpoint.Path = "/graphql"
	case strings.HasSuffix(path, "/api/v3"):
		endpoint.Path = strings.TrimSuffix(path, "/v3") + "/graphql"
	default:
		return "", errors.New("github: observe parent: configured API root has no supported GraphQL endpoint")
	}
	endpoint.RawPath = ""
	return endpoint.String(), nil
}

func (c *Client) parentGraphQLError(response *gogithub.Response, envelope parentIssueEnvelope) error {
	for _, failure := range envelope.Errors {
		if failure.Type == "RATE_LIMITED" || failure.Extensions.Type == "RATE_LIMITED" || failure.Extensions.Code == "RATE_LIMITED" {
			delay, reset := retryDelay(response.Header, c.clock())
			return &RateLimitError{
				APIError: APIError{Method: requestMethod(response.Response), URL: requestURL(response.Response),
					StatusCode: response.StatusCode, Message: "GraphQL parent observation rate limited", Err: ErrRateLimited},
				RetryAfter: delay, Reset: reset, Limit: headerInt(response.Header, "X-RateLimit-Limit"),
				Remaining: headerInt(response.Header, "X-RateLimit-Remaining"), Resource: "graphql",
			}
		}
	}
	return errors.New("github: observe parent: GraphQL errors prevent a complete parent observation")
}

// ObserveParent supplies a read-only optional relationship without changing
// Parent's existing mutation preconditions. It never resolves a source link or
// creates a local parent session.
func (a *Tickets) ObserveParent(ctx context.Context, child ticket.Ref) (*ticket.Ref, error) {
	if err := child.Validate(); err != nil {
		return nil, ticketError("observe parent", err, false)
	}
	repo, number, err := a.address(child.Locator)
	if err != nil {
		return nil, err
	}
	id := currentID(child)
	observed, err := a.client.ObserveParentIssue(ctx, repo, number, id)
	if err != nil {
		return nil, ticketError("observe parent", err, false)
	}
	if observed == nil {
		return nil, nil
	}
	ref := ticket.Ref{
		Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: strconv.FormatInt(observed.ID, 10)},
		Locator:  ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: observed.Repo.String(), Key: strconv.Itoa(observed.Number)},
	}
	return &ref, nil
}

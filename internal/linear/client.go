// Package linear implements factory-owned Linear ticket transport.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Provider identifies the Linear ticket service.
const Provider ticket.Provider = "linear"
const responseLimit = 4 << 20

// Config is nonsecret, version-bound connection configuration.
type Config struct {
	// RequestContext lets the daemon attach connection-local accounting and
	// refuse a new request during a hold. It must be safe for concurrent use.
	RequestContext func(context.Context) (context.Context, func(error), error)
	Origin         string
	WorkspaceID    string
	TeamID         string
	ProjectID      string
	AuthKind       string
	ModeLabels     map[string]string
	States         map[string]string
}

// Client sends one bounded request per operation; ledgers own retries.
type Client struct {
	config Config
	origin ticket.Origin
	token  secrets.Token
	http   *http.Client
}

// New constructs a connection-bound transport without revealing its credential.
func New(config Config, token secrets.Token, transport *http.Client) (*Client, error) {
	origin, err := ticket.CanonicalOrigin(config.Origin)
	if err != nil || token.Empty() || config.WorkspaceID == "" || config.TeamID == "" || config.ProjectID == "" {
		return nil, failure("configure", ticket.Refused, "canonical origin, credential and workspace/team/project IDs are required")
	}
	if config.AuthKind != "" && config.AuthKind != "api_key" && config.AuthKind != "oauth" {
		return nil, failure("configure", ticket.Refused, "unsupported authentication kind")
	}
	config.ModeLabels = maps.Clone(config.ModeLabels)
	config.States = maps.Clone(config.States)
	if transport == nil {
		transport = &http.Client{Timeout: 30 * time.Second}
	}
	copied := *transport
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copied.Timeout == 0 {
		copied.Timeout = 30 * time.Second
	}
	return &Client{config: config, origin: origin, token: token, http: &copied}, nil
}

// Provider returns the Linear ticket provider.
func (c *Client) Provider() ticket.Provider { return Provider }

// Origin returns the trusted configured API endpoint.
func (c *Client) Origin() ticket.Origin { return c.origin }

// Container returns the configured team/project scope.
func (c *Client) Container() string { return c.config.TeamID + "/" + c.config.ProjectID }
func (c *Client) identity(id string) ticket.Identity {
	return ticket.Identity{Provider: Provider, Origin: c.origin, RemoteID: id}
}
func failure(op string, kind ticket.FailureKind, message string) error {
	return &ticket.Failure{Operation: op, Kind: kind, Cause: errors.New(message)}
}

func (c *Client) query(ctx context.Context, operation, query string, variables any, mutation bool, out any) (resultErr error) {
	if c.config.RequestContext != nil {
		var err error
		var finish func(error)
		ctx, finish, err = c.config.RequestContext(ctx)
		if err != nil {
			return err
		}
		if finish != nil {
			defer func() { finish(resultErr) }()
		}
	}
	kind := ticket.Unavailable
	if mutation {
		kind = ticket.Uncertain
	}
	payload, err := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables"`
	}{query, variables})
	if err != nil {
		return failure(operation, ticket.Refused, "invalid request")
	}
	endpoint := string(c.origin)
	if !strings.HasSuffix(endpoint, "/graphql") {
		endpoint += "/graphql"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return failure(operation, ticket.Refused, "invalid request origin")
	}
	authorization := c.token.Reveal()
	if c.config.AuthKind == "oauth" {
		authorization = "Bearer " + authorization
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	observation := requestObservation(response)
	defer func() {
		var quota *ticket.RateLimitError
		if errors.As(resultErr, &quota) {
			observation.RetryAfter = quota.RetryAfter
		}
		if observe, ok := ctx.Value(requestObserverKey{}).(func(RequestObservation)); ok {
			observe(observation)
		}
	}()
	if err != nil {
		return failure(operation, kind, "provider transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return &ticket.Failure{Operation: operation, Kind: kind, Cause: &ticket.RateLimitError{RetryAfter: quotaDelay(response.Header, time.Now())}}
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadRequest {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			kind = ticket.Refused
		}
		return failure(operation, kind, "provider rejected request; inspect connection access or quota")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		return failure(operation, kind, "provider response incomplete or too large")
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Errors) > 0 {
		for _, rawError := range envelope.Errors {
			var detail struct {
				Message    string
				Extensions struct{ Code string }
			}
			if json.Unmarshal(rawError, &detail) != nil {
				continue
			}
			if detail.Extensions.Code == "RATELIMITED" {
				return &ticket.Failure{Operation: operation, Kind: kind, Cause: &ticket.RateLimitError{RetryAfter: quotaDelay(response.Header, time.Now())}}
			}
			if !mutation && (detail.Extensions.Code == "INVALID_CURSOR" || strings.Contains(strings.ToLower(detail.Message), "invalid cursor")) {
				return &ticket.Failure{Operation: operation, Kind: ticket.Refused, Cause: ticket.ErrInvalidCursor}
			}
		}
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(raw, &envelope) != nil || len(envelope.Errors) > 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return failure(operation, kind, "provider GraphQL response failed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(envelope.Data, &fields) != nil || len(fields) == 0 {
		return failure(operation, kind, "provider result missing")
	}
	for key, value := range fields {
		if string(value) == "null" {
			return failure(operation, kind, "provider result missing")
		}
		if key == "issues" || key == "comments" || key == "webhooks" {
			var page struct {
				Nodes    json.RawMessage
				PageInfo *struct {
					HasNextPage *bool
					EndCursor   string
				}
			}
			if json.Unmarshal(value, &page) != nil || len(page.Nodes) == 0 || string(page.Nodes) == "null" || page.PageInfo == nil || page.PageInfo.HasNextPage == nil {
				return failure(operation, kind, "provider page incomplete")
			}
		}
	}
	if json.Unmarshal(envelope.Data, out) != nil {
		return failure(operation, kind, "provider response malformed")
	}
	return nil
}

// quotaDelay reads scheduling headers without retaining provider prose. Linear's
// documented reset timestamps are epoch milliseconds; only exhausted buckets
// contribute reset delays. Missing advice uses a conservative one-minute hold.
func quotaDelay(header http.Header, now time.Time) time.Duration {
	delay := time.Duration(0)
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
		if seconds > 86400 {
			seconds = 86400
		}
		delay = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(header.Get("Retry-After")); err == nil && at.After(now) {
		delay = at.Sub(now)
	}
	for _, bucket := range []string{"Requests", "Endpoint-Requests", "Complexity"} {
		prefix := "X-RateLimit-" + bucket
		if header.Get(prefix+"-Remaining") != "0" {
			continue
		}
		if milliseconds, err := strconv.ParseInt(header.Get(prefix+"-Reset"), 10, 64); err == nil {
			if reset := time.UnixMilli(milliseconds); reset.After(now) && reset.Sub(now) > delay {
				delay = reset.Sub(now)
			}
		}
	}
	if delay <= 0 {
		return time.Minute
	}
	return min(delay, 24*time.Hour)
}

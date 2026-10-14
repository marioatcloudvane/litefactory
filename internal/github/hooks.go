package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/secrets"
)

// HookSecret is the shared secret GitHub signs webhook deliveries with. Like
// [secrets.Token] it prints as [redacted] under every formatting verb; the only
// place it is read is the request that registers it with GitHub.
type HookSecret struct {
	tok secrets.Token
}

// NewHookSecret wraps a secret value.
func NewHookSecret(value string) HookSecret { return HookSecret{tok: secrets.NewToken(value)} }

// Empty reports whether the secret carries no value.
func (s HookSecret) Empty() bool { return s.tok.Empty() }

// String implements fmt.Stringer.
func (s HookSecret) String() string { return secrets.Redacted }

// GoString implements fmt.GoStringer.
func (s HookSecret) GoString() string { return secrets.Redacted }

// Format implements fmt.Formatter, covering every verb.
func (s HookSecret) Format(state fmt.State, verb rune) {
	out := secrets.Redacted
	if verb == 'q' {
		out = strconv.Quote(out)
	}
	_, _ = io.WriteString(state, out)
}

// LogValue implements slog.LogValuer.
func (s HookSecret) LogValue() slog.Value { return slog.StringValue(secrets.Redacted) }

// MarshalJSON implements json.Marshaler.
func (s HookSecret) MarshalJSON() ([]byte, error) { return json.Marshal(secrets.Redacted) }

// HookContentTypeJSON is the only payload encoding litefactory registers.
const HookContentTypeJSON = "json"

// Hook is a repository webhook as GitHub reports it. GitHub never returns the
// secret, so there is none here.
type Hook struct {
	// ID is GitHub's hook id.
	ID int64
	// URL is the delivery URL.
	URL string
	// Active is whether GitHub delivers to the hook.
	Active bool
	// Events are the subscribed event names.
	Events []string
	// ContentType is "json" or "form".
	ContentType string
}

// HookSpec is the hook litefactory wants: sent whole on create and on edit.
// The content type is always json and certificate checking is always on.
type HookSpec struct {
	// URL is the delivery URL.
	URL string
	// Events are the event names to subscribe to.
	Events []string
	// Active turns delivery on.
	Active bool
	// Secret signs the deliveries.
	Secret HookSecret
}

func (spec HookSpec) payload() (*gogithub.Hook, error) {
	if spec.URL == "" || len(spec.Events) == 0 || spec.Secret.Empty() {
		return nil, fmt.Errorf("github: hook: %w: url, events and secret are required", ErrInvalidArgument)
	}
	return &gogithub.Hook{
		Active: gogithub.Ptr(spec.Active),
		Events: append([]string(nil), spec.Events...),
		Config: &gogithub.HookConfig{
			URL:         gogithub.Ptr(spec.URL),
			ContentType: gogithub.Ptr(HookContentTypeJSON),
			InsecureSSL: gogithub.Ptr("0"),
			Secret:      gogithub.Ptr(spec.Secret.tok.Reveal()), // hook-secret-site
		},
	}, nil
}

func convertHook(h *gogithub.Hook) Hook {
	if h == nil {
		return Hook{}
	}
	out := Hook{ID: h.GetID(), Active: h.GetActive(), Events: append([]string(nil), h.Events...)}
	if h.Config != nil {
		out.URL = h.Config.GetURL()
		out.ContentType = h.Config.GetContentType()
	}
	return out
}

// ListHooks lists a repository's webhooks (GET /repos/{o}/{r}/hooks), following
// pagination.
func (c *Client) ListHooks(ctx context.Context, repo Repo) ([]Hook, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	var out []Hook
	opts := &gogithub.ListOptions{PerPage: 100}
	for requests := 0; ; requests++ {
		if requests >= maxPages {
			return nil, fmt.Errorf("github: list hooks %s: %w", repo, ErrTooManyPages)
		}
		page, resp, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.Hook, *gogithub.Response, error) {
			return c.api.Repositories.ListHooks(ctx, repo.Owner, repo.Name, opts)
		})
		if err != nil {
			return nil, fmt.Errorf("github: list hooks %s: %w", repo, err)
		}
		for _, h := range page {
			out = append(out, convertHook(h))
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// GetHook reads one webhook (GET /repos/{o}/{r}/hooks/{id}). A hook that is
// gone is a [NotFoundError].
func (c *Client) GetHook(ctx context.Context, repo Repo, id int64) (Hook, error) {
	if err := repo.Validate(); err != nil {
		return Hook{}, err
	}
	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Hook, *gogithub.Response, error) {
		return c.api.Repositories.GetHook(ctx, repo.Owner, repo.Name, id)
	})
	if err != nil {
		return Hook{}, fmt.Errorf("github: get hook %d on %s: %w", id, repo, err)
	}
	return convertHook(payload), nil
}

// CreateHook registers a webhook (POST /repos/{o}/{r}/hooks).
func (c *Client) CreateHook(ctx context.Context, repo Repo, spec HookSpec) (Hook, error) {
	if err := repo.Validate(); err != nil {
		return Hook{}, err
	}
	body, err := spec.payload()
	if err != nil {
		return Hook{}, err
	}
	body.Name = gogithub.Ptr("web")
	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Hook, *gogithub.Response, error) {
		return c.api.Repositories.CreateHook(ctx, repo.Owner, repo.Name, body)
	})
	if err != nil {
		return Hook{}, fmt.Errorf("github: create hook on %s: %w", repo, err)
	}
	return convertHook(payload), nil
}

// EditHook replaces a webhook's configuration (PATCH /repos/{o}/{r}/hooks/{id}).
func (c *Client) EditHook(ctx context.Context, repo Repo, id int64, spec HookSpec) (Hook, error) {
	if err := repo.Validate(); err != nil {
		return Hook{}, err
	}
	body, err := spec.payload()
	if err != nil {
		return Hook{}, err
	}
	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Hook, *gogithub.Response, error) {
		return c.api.Repositories.EditHook(ctx, repo.Owner, repo.Name, id, body)
	})
	if err != nil {
		return Hook{}, fmt.Errorf("github: edit hook %d on %s: %w", id, repo, err)
	}
	return convertHook(payload), nil
}

// HookDelivery contains only recovery metadata, never request headers or bodies.
type HookDelivery struct {
	OriginalKnown bool
	ID            int64
	GUID          string
	DeliveredAt   time.Time
	Redelivery    bool
	StatusCode    int
	Event         string
	RepositoryID  int64
}

// HookDeliveryPage is one bounded page of available delivery history.
type HookDeliveryPage struct {
	Deliveries []HookDelivery
	Next       string
	ServerTime time.Time
}

func hookDelivery(h *gogithub.HookDelivery) HookDelivery {
	if h == nil {
		return HookDelivery{}
	}
	return HookDelivery{OriginalKnown: h.Redelivery != nil && !h.GetRedelivery(), ID: h.GetID(), GUID: h.GetGUID(), DeliveredAt: h.GetDeliveredAt().Time,
		Redelivery: h.GetRedelivery(), StatusCode: h.GetStatusCode(), Event: h.GetEvent(), RepositoryID: h.GetRepositoryID()}
}

// HookDeliveriesPage makes exactly one history request.
func (c *Client) HookDeliveriesPage(ctx context.Context, repo Repo, hook int64, cursor string) (HookDeliveryPage, error) {
	if err := repo.Validate(); err != nil {
		return HookDeliveryPage{}, err
	}
	items, resp, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.HookDelivery, *gogithub.Response, error) {
		return c.api.Repositories.ListHookDeliveries(ctx, repo.Owner, repo.Name, hook, &gogithub.ListCursorOptions{PerPage: 100, Cursor: cursor})
	})
	if err != nil {
		return HookDeliveryPage{}, err
	}
	page := HookDeliveryPage{}
	if resp != nil {
		page.Next = resp.Cursor
		page.ServerTime, _ = http.ParseTime(resp.Header.Get("Date"))
	}
	for _, item := range items {
		page.Deliveries = append(page.Deliveries, hookDelivery(item))
	}
	return page, nil
}

// HookDeliveryIssue reads a delivery's source-local issue locator. Payloads are
// discarded here; callers must verify identity and reread authoritative state.
func (c *Client) HookDeliveryIssue(ctx context.Context, repo Repo, hook, delivery int64) (HookDelivery, string, int, error) {
	if err := repo.Validate(); err != nil {
		return HookDelivery{}, "", 0, err
	}
	item, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.HookDelivery, *gogithub.Response, error) {
		return c.api.Repositories.GetHookDelivery(ctx, repo.Owner, repo.Name, hook, delivery)
	})
	if err != nil {
		return HookDelivery{}, "", 0, err
	}
	var payload struct {
		Repository struct {
			FullName string `json:"full_name"`
		}
		Issue struct {
			Number int
			URL    string
		}
	}
	if item == nil || item.Request == nil || item.Request.RawPayload == nil {
		return HookDelivery{}, "", 0, ErrInvalidArgument
	}
	if err := json.Unmarshal(*item.Request.RawPayload, &payload); err != nil {
		return HookDelivery{}, "", 0, ErrInvalidArgument
	}
	if payload.Repository.FullName != repo.String() || payload.Issue.Number <= 0 {
		return HookDelivery{}, "", 0, ErrInvalidArgument
	}
	return hookDelivery(item), payload.Issue.URL, payload.Issue.Number, nil
}

// RedeliverHookDelivery requests one attempt. A nil error acknowledges the
// request only; it is not evidence of delivery or consumption.
func (c *Client) RedeliverHookDelivery(ctx context.Context, repo Repo, hook, delivery int64) (HookDelivery, error) {
	if err := repo.Validate(); err != nil {
		return HookDelivery{}, err
	}
	item, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.HookDelivery, *gogithub.Response, error) {
		item, resp, err := c.api.Repositories.RedeliverHookDelivery(ctx, repo.Owner, repo.Name, hook, delivery)
		var accepted *gogithub.AcceptedError
		if resp != nil && resp.StatusCode == http.StatusAccepted && errors.As(err, &accepted) {
			err = nil
			// Some responses identify the new attempt. Only that explicit
			// identity can later reconcile this request without inference.
			var receipt gogithub.HookDelivery
			if json.Unmarshal(accepted.Raw, &receipt) == nil {
				item = &receipt
			}
		}
		return item, resp, err
	})
	return hookDelivery(item), err
}

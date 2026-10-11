package linear

import (
	"context"
	"net/url"
	"slices"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const webhookFields = "id url enabled resourceTypes team { id } allPublicTeams"

type webhook struct {
	ID, URL                 string
	Enabled, AllPublicTeams bool
	ResourceTypes           []string
	Team                    *objectID
}

func (c *Client) subscription(w webhook) (ticket.Subscription, error) {
	if w.ID == "" || w.Team == nil || w.Team.ID != c.config.TeamID || w.AllPublicTeams || w.URL == "" {
		return ticket.Subscription{}, failure("subscription", ticket.Conflict, "subscription identity or team differs")
	}
	return ticket.Subscription{ID: c.identity(w.ID), Container: c.Container(), CallbackURL: w.URL, Events: w.ResourceTypes, Enabled: w.Enabled, ContentType: "json"}, nil
}

// ListSubscriptions reads team-scoped subscriptions without adopting them.
func (c *Client) ListSubscriptions(ctx context.Context, container string) ([]ticket.Subscription, error) {
	if container != c.Container() {
		return nil, failure("subscriptions", ticket.Refused, "container differs")
	}
	out := []ticket.Subscription{}
	after := ""
	for range 100 {
		var data struct {
			Webhooks struct {
				Nodes    []webhook
				PageInfo pageInfo
			}
		}
		if err := c.query(ctx, "subscriptions", "query($after: String) { webhooks(first: 100, after: $after) { nodes { "+webhookFields+" } pageInfo { hasNextPage endCursor } } }", map[string]any{"after": nullable(after)}, false, &data); err != nil {
			return nil, err
		}
		if len(data.Webhooks.Nodes) > 100 {
			return nil, failure("subscriptions", ticket.Unavailable, "provider exceeded page bound")
		}
		for _, w := range data.Webhooks.Nodes {
			if w.Team == nil || w.Team.ID != c.config.TeamID || w.AllPublicTeams {
				continue
			}
			s, err := c.subscription(w)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		if !data.Webhooks.PageInfo.HasNextPage {
			return out, nil
		}
		if data.Webhooks.PageInfo.EndCursor == "" || data.Webhooks.PageInfo.EndCursor == after {
			return nil, failure("subscriptions", ticket.Unavailable, "provider continuation missing or repeated")
		}
		after = data.Webhooks.PageInfo.EndCursor
	}
	return nil, failure("subscriptions", ticket.Unavailable, "subscription page bound exceeded")
}

// GetSubscription reads an exact qualified team-scoped webhook.
func (c *Client) GetSubscription(ctx context.Context, container string, id ticket.Identity) (ticket.Subscription, error) {
	if container != c.Container() || !id.Equal(c.identity(id.RemoteID)) {
		return ticket.Subscription{}, failure("subscription", ticket.Refused, "subscription belongs to another connection")
	}
	var data struct{ Webhook webhook }
	if err := c.query(ctx, "subscription", "query($id: String!) { webhook(id: $id) { "+webhookFields+" } }", map[string]string{"id": id.RemoteID}, false, &data); err != nil {
		return ticket.Subscription{}, err
	}
	if data.Webhook.ID != id.RemoteID {
		return ticket.Subscription{}, failure("subscription", ticket.Conflict, "subscription identity differs")
	}
	return c.subscription(data.Webhook)
}
func validSpec(spec ticket.SubscriptionSpec) bool {
	u, err := url.Parse(spec.CallbackURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(spec.Events) != 2 {
		return false
	}
	return slices.Contains(spec.Events, "Issue") && slices.Contains(spec.Events, "Comment")
}

// CreateSubscription dispatches one webhook creation; its owner reconciles uncertainty.
func (c *Client) CreateSubscription(ctx context.Context, container string, spec ticket.SubscriptionSpec, secret secrets.Token) (ticket.Subscription, error) {
	if container != c.Container() || !validSpec(spec) || secret.Empty() {
		return ticket.Subscription{}, failure("create subscription", ticket.Refused, "team scope, https callback, Issue/Comment events and signing secret required")
	}
	var data struct {
		WebhookCreate struct {
			Success bool
			Webhook webhook
		}
	}
	err := c.query(ctx, "create subscription", "mutation($input: WebhookCreateInput!) { webhookCreate(input: $input) { success webhook { "+webhookFields+" } } }", map[string]any{"input": map[string]any{"teamId": c.config.TeamID, "url": spec.CallbackURL, "resourceTypes": spec.Events, "enabled": spec.Enabled, "secret": secret.Reveal(), "allPublicTeams": false}}, true, &data)
	if err != nil {
		return ticket.Subscription{}, err
	}
	if !data.WebhookCreate.Success {
		return ticket.Subscription{}, failure("create subscription", ticket.Uncertain, "mutation confirmation missing")
	}
	observed, err := c.subscription(data.WebhookCreate.Webhook)
	if err != nil {
		return ticket.Subscription{}, failure("create subscription", ticket.Uncertain, "mutation receipt scope differs")
	}
	return observed, nil
}

// UpdateSubscription changes only an exact caller-owned subscription after a scoped read.
func (c *Client) UpdateSubscription(ctx context.Context, container string, id ticket.Identity, spec ticket.SubscriptionSpec, secret secrets.Token) (ticket.Subscription, error) {
	if !validSpec(spec) || secret.Empty() {
		return ticket.Subscription{}, failure("update subscription", ticket.Refused, "valid subscription and signing secret required")
	}
	if _, err := c.GetSubscription(ctx, container, id); err != nil {
		return ticket.Subscription{}, err
	}
	var data struct {
		WebhookUpdate struct {
			Success bool
			Webhook webhook
		}
	}
	err := c.query(ctx, "update subscription", "mutation($id: String!, $input: WebhookUpdateInput!) { webhookUpdate(id: $id, input: $input) { success webhook { "+webhookFields+" } } }", map[string]any{"id": id.RemoteID, "input": map[string]any{"url": spec.CallbackURL, "resourceTypes": spec.Events, "enabled": spec.Enabled, "secret": secret.Reveal()}}, true, &data)
	if err != nil {
		return ticket.Subscription{}, err
	}
	if !data.WebhookUpdate.Success || data.WebhookUpdate.Webhook.ID != id.RemoteID {
		return ticket.Subscription{}, failure("update subscription", ticket.Uncertain, "mutation confirmation missing")
	}
	observed, err := c.subscription(data.WebhookUpdate.Webhook)
	if err != nil {
		return ticket.Subscription{}, failure("update subscription", ticket.Uncertain, "mutation receipt scope differs")
	}
	return observed, nil
}

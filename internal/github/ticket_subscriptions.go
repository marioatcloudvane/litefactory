package github

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func (a *Tickets) subscription(repo Repo, hook Hook) (ticket.Subscription, error) {
	if hook.ID < 1 {
		return ticket.Subscription{}, ticketError("subscription", ErrUnexpectedStatus, false)
	}
	return ticket.Subscription{ID: ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: strconv.FormatInt(hook.ID, 10)}, Container: repo.String(), CallbackURL: hook.URL, Events: hook.Events, Enabled: hook.Active, ContentType: hook.ContentType}, nil
}

func (a *Tickets) subscriptionAddress(container string, id ticket.Identity) (Repo, int64, error) {
	repo, _, err := a.address(ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"})
	if err != nil {
		return Repo{}, 0, err
	}
	number, err := strconv.ParseInt(id.RemoteID, 10, 64)
	if id.Validate() != nil || id.Provider != ticket.GitHub || id.Origin != a.origin || err != nil || number < 1 || strconv.FormatInt(number, 10) != id.RemoteID {
		return Repo{}, 0, ticketError("subscription identity", ErrInvalidArgument, false)
	}
	return repo, number, nil
}

// ListSubscriptions completes the existing bounded hook listing. It exposes no
// secret and does not adopt or mutate hooks; ownership belongs to the registrar.
func (a *Tickets) ListSubscriptions(ctx context.Context, container string) ([]ticket.Subscription, error) {
	repo, _, err := a.address(ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"})
	if err != nil {
		return nil, err
	}
	hooks, err := a.client.ListHooks(ctx, repo)
	if err != nil {
		return nil, ticketError("list subscriptions", err, false)
	}
	out := make([]ticket.Subscription, 0, len(hooks))
	for _, hook := range hooks {
		item, err := a.subscription(repo, hook)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// GetSubscription retrieves an exact qualified hook without changing ownership.
func (a *Tickets) GetSubscription(ctx context.Context, container string, id ticket.Identity) (ticket.Subscription, error) {
	repo, number, err := a.subscriptionAddress(container, id)
	if err != nil {
		return ticket.Subscription{}, err
	}
	hook, err := a.client.GetHook(ctx, repo, number)
	if err != nil {
		return ticket.Subscription{}, ticketError("read subscription", err, false)
	}
	if hook.ID != number {
		return ticket.Subscription{}, ticketError("read subscription", ErrUnexpectedStatus, false)
	}
	return a.subscription(repo, hook)
}

// CreateSubscription performs exactly one effect; a lost response remains
// uncertain. The registrar records intent before calling and owns reconciliation.
func (a *Tickets) CreateSubscription(ctx context.Context, container string, spec ticket.SubscriptionSpec, secret secrets.Token) (ticket.Subscription, error) {
	repo, _, err := a.address(ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"})
	if err != nil {
		return ticket.Subscription{}, err
	}
	hook, err := a.client.CreateHook(ctx, repo, HookSpec{URL: spec.CallbackURL, Events: spec.Events, Active: spec.Enabled, Secret: HookSecret{tok: secret}})
	if err != nil {
		return ticket.Subscription{}, ticketError("create subscription", err, true)
	}
	out, err := a.subscription(repo, hook)
	if err != nil {
		return ticket.Subscription{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "create subscription", Cause: err}
	}
	return out, nil
}

// UpdateSubscription changes only the caller-owned exact qualified hook ID.
// Neither this adapter nor the client retries a possibly dispatched mutation.
func (a *Tickets) UpdateSubscription(ctx context.Context, container string, id ticket.Identity, spec ticket.SubscriptionSpec, secret secrets.Token) (ticket.Subscription, error) {
	repo, number, err := a.subscriptionAddress(container, id)
	if err != nil {
		return ticket.Subscription{}, err
	}
	hook, err := a.client.EditHook(ctx, repo, number, HookSpec{URL: spec.CallbackURL, Events: spec.Events, Active: spec.Enabled, Secret: HookSecret{tok: secret}})
	if err != nil {
		return ticket.Subscription{}, ticketError("update subscription", err, true)
	}
	if hook.ID != number {
		return ticket.Subscription{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "update subscription", Cause: ErrUnexpectedStatus}
	}
	return a.subscription(repo, hook)
}

// ContainerIdentity reads the immutable repository identity through the adapter's
// trusted configured origin. Payload repository names alone cannot establish it.
func (a *Tickets) ContainerIdentity(ctx context.Context, container string) (ticket.Identity, error) {
	repo, _, err := a.address(ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"})
	if err != nil {
		return ticket.Identity{}, err
	}
	observed, err := a.client.InspectRepository(ctx, repo)
	if err != nil {
		return ticket.Identity{}, ticketError("read container", err, false)
	}
	if !strings.EqualFold(observed.FullName, repo.String()) {
		return ticket.Identity{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "read container", Cause: errors.New("repository name differs from configured source")}
	}
	return ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: strconv.FormatInt(observed.ID, 10)}, nil
}

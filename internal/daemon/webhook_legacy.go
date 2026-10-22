package daemon

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

// Legacy import requires a verified original connection. Provider verification
// happens outside HTTP ingress, and failures leave
// polling available without assigning an old secret to a replacement source.
func (d *Daemon) importLegacyWebhookSubscriptions(ctx context.Context, now time.Time) {
	if d.webhook == nil || d.connections == nil {
		return
	}
	repos, err := d.db.ListRepos(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "legacy webhook import deferred", slog.Any("error", err))
		return
	}
	for _, repo := range repos {
		version, err := d.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
		if err != nil || !version.Enabled || version.Provenance != "legacy" {
			continue
		}
		subscriptions, err := d.db.ListTicketSubscriptions(ctx, repo.ID)
		if err != nil {
			continue
		}
		var existing *store.TicketSubscription
		for i := range subscriptions {
			if subscriptions[i].ConnectionVersionID == version.ID {
				existing = &subscriptions[i]
				break
			}
		}
		// An imported legacy row still needs its existing hook adopted, or
		// webhook recovery never knows the hook and polls every minute.
		if existing != nil && (existing.RemoteHookID != "" || !existing.DesiredEnabled) {
			continue
		}
		var containerID string
		key := "legacy/" + repo.Slug()
		d.webhook.mu.Lock()
		previous := d.webhook.subscriptionSecretTries[key]
		if previous.revision == version.ID && now.Sub(previous.at) < webhookSecretRetry {
			d.webhook.mu.Unlock()
			continue
		}
		d.webhook.subscriptionSecretTries[key] = subscriptionSecretAttempt{revision: version.ID, at: now}
		d.webhook.mu.Unlock()
		clients, err := d.connections.Clients(ctx, version, 0)
		if err == nil {
			var identity ticket.Identity
			if identity, err = clients.Tickets.ContainerIdentity(ctx, version.Destination); err == nil && (identity.Provider != version.Provider || identity.Origin != version.Origin) {
				err = errors.New("ticket source identity changed")
			}
			containerID = identity.RemoteID
		}
		if err != nil {
			d.log.WarnContext(ctx, "legacy webhook source unverified; polling continues", slog.String("repo", repo.Slug()), slog.Any("error", err))
			continue
		}
		var saved store.TicketSubscription
		if existing != nil {
			saved = *existing
		} else {
			saved, err = d.db.ImportLegacyTicketSubscription(ctx, version, containerID)
		}
		if errors.Is(err, store.ErrNotFound) {
			token, secretErr := d.webhook.resolver.Resolve(ctx, secrets.KeychainRef(WebhookKeychainService, repo.Slug()))
			if secretErr != nil || token.Reveal() == "" {
				d.log.WarnContext(ctx, "webhook signing secret unavailable; polling continues", slog.String("repo", repo.Slug()), slog.String("remedy", "litefactory repo webhook "+repo.Slug()))
				continue
			}
			saved, err = d.db.PrepareLegacyTicketSubscription(ctx, version, containerID, d.webhook.publicURL)
		}
		if err == nil {
			registrar := webhookreg.Subscriptions{Store: d.db, Forge: clients.Tickets, Keychain: webhookreg.SystemKeychain{Resolver: d.webhook.resolver}, PublicURL: d.webhook.publicURL}
			if saved, err = registrar.AdoptLegacyHook(ctx, saved); err == nil && saved.RemoteHookID != "" {
				d.log.InfoContext(ctx, "legacy webhook adopted", slog.String("repo", repo.Slug()), slog.String("hook", saved.RemoteHookID))
			}
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			d.log.WarnContext(ctx, "legacy webhook ownership is unresolved; polling continues", slog.Int64("project", repo.ID), slog.Any("error", err))
		}
	}
}

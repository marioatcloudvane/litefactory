package daemon

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Legacy import requires a single original connection and a recorded owned
// hook. Provider verification happens outside HTTP ingress, and failures leave
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
		exists := false
		for _, sub := range subscriptions {
			if sub.ConnectionVersionID == version.ID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if _, err := d.db.GetRepoWebhook(ctx, repo.ID); err != nil {
			continue
		}
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
		if err != nil {
			continue
		}
		identity, err := clients.Tickets.ContainerIdentity(ctx, version.Destination)
		if err != nil {
			continue
		}
		if _, err := d.db.ImportLegacyTicketSubscription(ctx, version, identity.RemoteID); err != nil && !errors.Is(err, store.ErrNotFound) {
			d.log.WarnContext(ctx, "legacy webhook ownership is unresolved; polling continues", slog.Int64("project", repo.ID))
		}
	}
}

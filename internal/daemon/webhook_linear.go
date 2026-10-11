package daemon

import (
	"context"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/webhook"
)

func (w *webhookRuntime) WakeOpaqueSubscription(source webhook.Source, id string) {
	w.hints.add(hintKey{repoID: source.RepoID, versionID: source.ConnectionVersionID, revision: source.Revision, selector: source.Selector, ticketKey: id})
	if w.schedule != nil {
		w.schedule.WebhookAccepted(source.RepoID, w.now())
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
func (d *Daemon) handoffOpaqueWebhook(ctx context.Context, hint hintKey, q *repoQueue) error {
	version, err := d.db.GetConnectionVersion(ctx, hint.versionID)
	if err != nil {
		return err
	}
	if version.Provider != ticket.Linear {
		return store.ErrTicketIdentityConflict
	}
	clients, err := d.connections.Clients(ctx, version, 0)
	if err != nil {
		return err
	}
	snapshot, err := doctorCall(ctx, d, func(ctx context.Context) (ticket.Snapshot, error) {
		return clients.Tickets.Resolve(ctx, ticket.Locator{Provider: version.Provider, Origin: version.Origin, Container: version.Destination, Key: hint.ticketKey})
	})
	if err != nil {
		return err
	}
	if snapshot.Ref.Identity.Provider != version.Provider || snapshot.Ref.Identity.Origin != version.Origin || snapshot.Ref.Locator.Container != version.Destination {
		return store.ErrTicketIdentityConflict
	}
	position, err := d.db.GetTicketRecoveryPosition(ctx, version.ID)
	if err != nil {
		return err
	}
	observation := store.PendingTicketObservation{ConnectionVersionID: version.ID, RepoID: q.repo.ID, Provider: version.Provider, Origin: version.Origin, Container: version.Destination, ContainerID: position.ContainerID, TicketID: snapshot.Ref.Identity.RemoteID, TicketKey: snapshot.Ref.Locator.Key, ObservedAt: d.webhook.now()}
	return d.db.HandoffSubscriptionObservation(ctx, hint.selector, hint.revision, observation)
}

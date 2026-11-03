package daemon

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/factory/ticketbridge"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func ingressProvider(a TicketIngress) ticket.Provider {
	if p, ok := a.(interface{ Provider() ticket.Provider }); ok {
		return p.Provider()
	}
	return ticket.GitHub
}
func (d *Daemon) ticketLocator(ctx context.Context, a TicketIngress, origin ticket.Origin, r github.Repo, n int) (ticket.Locator, error) {
	if ingressProvider(a) == ticket.GitHub {
		return locatorAt(origin, r, n), nil
	}
	id, err := d.db.TicketIdentityForSlot(ctx, "issue", int64(n))
	if err != nil {
		return ticket.Locator{}, err
	}
	if id.Provider != ticket.Linear || id.Origin != origin {
		return ticket.Locator{}, store.ErrTicketIdentityConflict
	}
	return ticket.Locator{Provider: id.Provider, Origin: id.Origin, Container: r.String(), Key: id.RemoteID}, nil
}
func (d *Daemon) legacySnapshot(ctx context.Context, snapshot ticket.Snapshot) (github.Issue, error) {
	return ticketbridge.New(d.db).Snapshot(ctx, snapshot)
}

func (d *Daemon) legacyReply(ctx context.Context, reply ticket.Reply) (github.Comment, error) {
	return ticketbridge.New(d.db).Reply(ctx, reply)
}
func (d *Daemon) bindingRef(ctx context.Context, a TicketIngress, origin ticket.Origin, r github.Repo, i github.Issue) (ticket.Ref, error) {
	if ingressProvider(a) == ticket.GitHub {
		return ticket.Ref{Locator: locatorAt(origin, r, i.Number), Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: strconv.FormatInt(i.ID, 10)}}, nil
	}
	l, err := d.ticketLocator(ctx, a, origin, r, i.Number)
	if err != nil {
		return ticket.Ref{}, err
	}
	id, err := d.db.TicketIdentityForSlot(ctx, "issue", i.ID)
	if err != nil {
		return ticket.Ref{}, err
	}
	if id.RemoteID != l.Key {
		return ticket.Ref{}, fmt.Errorf("local ticket slot differs")
	}
	return ticket.Ref{Locator: l, Identity: id}, nil
}

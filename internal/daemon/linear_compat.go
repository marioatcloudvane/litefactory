package daemon

import (
	"context"
	"fmt"
	"strconv"

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
func (d *Daemon) legacySnapshot(ctx context.Context, s ticket.Snapshot) (github.Issue, error) {
	if s.Ref.Identity.Provider == ticket.GitHub {
		return legacyTicketSnapshot(s)
	}
	if s.Ref.Identity.Provider != ticket.Linear || s.Ref.Validate() != nil {
		return github.Issue{}, store.ErrTicketIdentityConflict
	}
	slot, err := d.db.LocalTicketSlot(ctx, "issue", s.Ref.Identity)
	if err != nil {
		return github.Issue{}, err
	}
	author, err := d.legacyActor(ctx, s.Author)
	if err != nil {
		return github.Issue{}, err
	}
	copySnapshot := s
	out := github.Issue{QualifiedSnapshot: &copySnapshot, ID: slot, Number: int(slot), Title: s.Title, Body: s.Body, State: s.State, StateReason: s.StateReason, Author: author, Labels: s.Labels, HTMLURL: s.BrowserURL, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	for _, a := range s.Assignees {
		u, e := d.legacyActor(ctx, a)
		if e != nil {
			return github.Issue{}, e
		}
		out.Assignees = append(out.Assignees, u)
	}
	return out, nil
}
func (d *Daemon) legacyActor(ctx context.Context, a ticket.Actor) (github.User, error) {
	if a.Identity.RemoteID == "" {
		return github.User{}, nil
	}
	slot, err := d.db.LocalTicketSlot(ctx, "actor", a.Identity)
	return github.User{ID: slot, Login: a.Handle, IsBot: a.IsBot, IsHuman: !a.IsBot}, err
}
func (d *Daemon) legacyReply(ctx context.Context, r ticket.Reply) (github.Comment, error) {
	slot, err := d.db.LocalTicketSlot(ctx, "comment", r.Identity)
	if err != nil {
		return github.Comment{}, err
	}
	author, err := d.legacyActor(ctx, r.Author)
	if err != nil {
		return github.Comment{}, err
	}
	copyReply := r
	return github.Comment{ID: slot, Author: author, Body: r.Body, HTMLURL: r.BrowserURL, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, QualifiedReply: &copyReply}, nil
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

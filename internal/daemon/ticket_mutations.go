package daemon

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type neutralLabels interface {
	AddLabel(context.Context, ticket.Ref, string) error
	RemoveLabel(context.Context, ticket.Ref, string) error
}
type ticketLabelBridge struct {
	daemon  *Daemon
	adapter neutralLabels
}

func (b ticketLabelBridge) AddIssueLabel(ctx context.Context, repo github.Repo, number int, label string) error {
	s, err := b.daemon.resolvedTicket(ctx, repo, number)
	if err != nil {
		return err
	}
	return b.adapter.AddLabel(ctx, s.Ref, label)
}
func (b ticketLabelBridge) RemoveIssueLabel(ctx context.Context, repo github.Repo, number int, label string) error {
	s, err := b.daemon.resolvedTicket(ctx, repo, number)
	if err != nil {
		return err
	}
	return b.adapter.RemoveLabel(ctx, s.Ref, label)
}
func (d *Daemon) ticketLabels() (labelForge, bool) {
	if d.tickets == nil {
		labels, ok := d.forge.(labelForge)
		return labels, ok
	}
	labels, ok := d.tickets.(neutralLabels)
	return ticketLabelBridge{daemon: d, adapter: labels}, ok
}
func (d *Daemon) reactToComment(ctx context.Context, repo github.Repo, number int, commentID int64, content github.ReactionContent) error {
	if d.tickets == nil {
		return d.forge.AddCommentReaction(ctx, repo, commentID, content)
	}
	adapter, ok := d.tickets.(interface {
		React(context.Context, ticket.Reply, string) error
	})
	if !ok {
		return fmt.Errorf("daemon: ticket reaction contract required")
	}
	snapshot, err := d.resolvedTicket(ctx, repo, number)
	if err != nil {
		return err
	}
	reply := ticket.Reply{Ticket: snapshot.Ref, Identity: ticket.Identity{Provider: snapshot.Ref.Identity.Provider, Origin: snapshot.Ref.Identity.Origin, RemoteID: strconv.FormatInt(commentID, 10)}}
	return adapter.React(ctx, reply, string(content))
}

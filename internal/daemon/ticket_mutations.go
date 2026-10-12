package daemon

import (
	"context"
	"fmt"

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

func (b ticketLabelBridge) labels(ctx context.Context, repo github.Repo, number int) (neutralLabels, error) {
	if b.daemon.connections == nil {
		return b.adapter, nil
	}
	ingress, _, err := b.daemon.connectionIngress(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	labels, ok := ingress.(neutralLabels)
	if !ok {
		return nil, fmt.Errorf("daemon: ticket label contract required")
	}
	return labels, nil
}

func (b ticketLabelBridge) AddIssueLabel(ctx context.Context, repo github.Repo, number int, label string) error {
	adapter, err := b.labels(ctx, repo, number)
	if err != nil {
		return err
	}
	s, err := b.daemon.resolvedTicket(ctx, repo, number)
	if err != nil {
		return err
	}
	return adapter.AddLabel(ctx, s.Ref, label)
}
func (b ticketLabelBridge) RemoveIssueLabel(ctx context.Context, repo github.Repo, number int, label string) error {
	adapter, err := b.labels(ctx, repo, number)
	if err != nil {
		return err
	}
	s, err := b.daemon.resolvedTicket(ctx, repo, number)
	if err != nil {
		return err
	}
	return adapter.RemoveLabel(ctx, s.Ref, label)
}
func (d *Daemon) ticketLabels() (labelForge, bool) {
	if d.connections != nil {
		return ticketLabelBridge{daemon: d}, true
	}
	if d.tickets == nil {
		labels, ok := d.forge.(labelForge)
		return labels, ok
	}
	labels, ok := d.tickets.(neutralLabels)
	return ticketLabelBridge{daemon: d, adapter: labels}, ok
}
func (d *Daemon) reactToComment(ctx context.Context, repo github.Repo, number int, commentID int64, content github.ReactionContent) error {
	if d.tickets == nil && d.connections == nil {
		return d.forge.AddCommentReaction(ctx, repo, commentID, content)
	}
	ingress, _, err := d.connectionIngress(ctx, repo, number)
	if err != nil {
		return err
	}
	adapter, ok := ingress.(interface {
		React(context.Context, ticket.Reply, string) error
	})
	if !ok {
		return fmt.Errorf("daemon: ticket reaction contract required")
	}
	snapshot, err := d.resolvedTicket(ctx, repo, number)
	if err != nil {
		return err
	}
	comments, err := d.readComments(ctx, repo, number, 0)
	if err != nil {
		return err
	}
	for _, observed := range comments {
		if observed.ID != commentID {
			continue
		}
		reply, err := ingress.QualifyReply(snapshot.Ref, observed)
		if err != nil {
			return err
		}
		return adapter.React(ctx, reply, string(content))
	}
	return fmt.Errorf("daemon: reaction reply is not on the recorded ticket")
}

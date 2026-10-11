package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TicketIngress is the narrow verified-observation boundary. GitHub payload
// conversion remains an explicit legacy ingress operation, not ticket authority.
type TicketIngress interface {
	Resolve(context.Context, ticket.Locator) (ticket.Snapshot, error)
	QualifyReply(ticket.Ref, github.Comment) (ticket.Reply, error)
}

type qualifiedClassifier interface {
	AdmitReply(context.Context, int64, ticket.Reply) (turn.Admission, error)
}

func (d *Daemon) qualifyReply(ctx context.Context, sessionID int64, c github.Comment) (*ticket.Reply, error) {
	if d.tickets == nil {
		return nil, nil
	}
	ingress := d.tickets
	origin := d.filter.Identities().TicketOrigin
	if d.connections != nil {
		mapping, e := d.db.GetSessionTicket(ctx, sessionID)
		if e != nil {
			return nil, e
		}
		parts := strings.Split(mapping.Locator.Container, "/")
		selected, _, e := selectTicketConnection(ctx, d.db, d.connections, github.Repo{Owner: parts[0], Name: parts[1]}, 0, sessionID)
		if e != nil {
			return nil, e
		}
		ingress, origin = selected, selected.Origin()
	}
	if origin == "" {
		return nil, errors.New("daemon: ticket origin is required for qualified ingress")
	}
	binding, err := d.db.BindLegacySessionOrigin(ctx, sessionID, origin)
	if err != nil {
		return nil, err
	}
	snapshot, err := ingress.Resolve(ctx, binding.Locator)
	if err != nil {
		return nil, err
	}
	binding, err = d.db.BindSessionTicket(ctx, sessionID, snapshot.Ref)
	if err != nil {
		return nil, err
	}
	ref, err := binding.Ref()
	if err != nil {
		return nil, err
	}
	reply, err := ingress.QualifyReply(ref, c)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

func (d *Daemon) admitReply(ctx context.Context, sessionID int64, c github.Comment, reply *ticket.Reply) (turn.Admission, error) {
	if reply == nil {
		return d.filter.Admit(ctx, sessionID, c)
	}
	classifier, ok := d.filter.(qualifiedClassifier)
	if !ok {
		return turn.Admission{}, errors.New("daemon: qualified classifier is required")
	}
	return classifier.AdmitReply(ctx, sessionID, *reply)
}

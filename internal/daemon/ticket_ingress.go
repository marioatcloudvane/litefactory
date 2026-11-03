package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"

	factoryruntime "github.com/cloudvane/litefactory/internal/factory/runtime"
	"github.com/cloudvane/litefactory/internal/factory/ticketbridge"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TicketIngress is the narrow verified-observation boundary. GitHub payload
// conversion remains an explicit legacy ingress operation, not ticket authority.
type TicketIngress = ticketbridge.Ingress

type qualifiedClassifier interface {
	AdmitReply(context.Context, int64, ticket.Reply) (turn.Admission, error)
}

func (d *Daemon) qualifyReply(ctx context.Context, sessionID int64, c github.Comment) (*ticket.Reply, error) {
	if d.tickets == nil && d.connections == nil {
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
	binding, err := d.db.GetSessionTicket(ctx, sessionID)
	if ingressProvider(ingress) == ticket.GitHub {
		binding, err = d.db.BindLegacySessionOrigin(ctx, sessionID, origin)
	}
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
	if d.connections != nil {
		return factoryruntime.NewTicketAdmission(d.db, d.log).Admit(ctx, sessionID, reply)
	}
	if reply == nil {
		return d.filter.Admit(ctx, sessionID, c)
	}
	classifier, ok := d.filter.(qualifiedClassifier)
	if !ok {
		return turn.Admission{}, errors.New("daemon: qualified classifier is required")
	}
	return classifier.AdmitReply(ctx, sessionID, *reply)
}

func (d *Daemon) projectTicketIdentities(ctx context.Context, sessionID int64) (turn.Identities, error) {
	return factoryruntime.NewTicketAdmission(d.db, d.log).Identities(ctx, sessionID)
}

// ticketIdentities never falls back to installation authority for project work.
func (d *Daemon) ticketIdentities(ctx context.Context) turn.Identities {
	if d.connections == nil {
		return d.filter.Identities()
	}
	scope, _ := ctx.Value(connectionScopeKey{}).(connectionScope)
	ids, err := d.projectTicketIdentities(ctx, scope.sessionID)
	if err != nil {
		if d.log != nil {
			d.log.ErrorContext(ctx, "project ticket authority unavailable", "error", err)
		}
		return turn.Identities{}
	}
	return ids
}

// eligibleProjectComment applies the activation boundary to managed child input.
// These comments are read through the parent's retained qualified ticket client.
func (d *Daemon) eligibleProjectComment(ctx context.Context, sessionID int64, c github.Comment) (bool, error) {
	if d.connections == nil {
		return true, nil
	}
	return factoryruntime.NewTicketAdmission(d.db, d.log).EligibleComment(ctx, sessionID, c)
}

func (d *Daemon) projectTrigger(ctx context.Context, sessionID int64, c github.Comment) (turn.Trigger, error) {
	trigger := turn.Trigger{ReplyToCommentID: c.ReplyToCommentID, CommentID: c.ID, Body: c.Body, Author: c.Author.Login, AuthorID: c.Author.ID, CreatedAt: c.CreatedAt}
	if d.connections == nil {
		return trigger, nil
	}
	mapping, err := d.db.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return trigger, err
	}
	if c.QualifiedReply != nil {
		trigger.Actor = c.QualifiedReply.Author.Identity
		trigger.Source = c.QualifiedReply.Identity
		return trigger, nil
	}
	trigger.Actor = ticket.Identity{Provider: mapping.Locator.Provider, Origin: mapping.Locator.Origin, RemoteID: strconv.FormatInt(c.Author.ID, 10)}
	trigger.Source = ticket.Identity{Provider: mapping.Locator.Provider, Origin: mapping.Locator.Origin, RemoteID: strconv.FormatInt(c.ID, 10)}
	return trigger, nil
}

func (d *Daemon) recordCodeObservation(ctx context.Context, sessionID int64, c github.Comment, reply *ticket.Reply, authorized bool) error {
	if d.connections == nil {
		if authorized {
			_, err := d.filter.Admit(ctx, sessionID, c)
			return err
		}
		_, err := d.filter.Admit(ctx, sessionID, c)
		return err
	}
	if reply == nil {
		return errors.New("qualified code-control source is required")
	}
	saved, err := d.db.GetExecutionSnapshot(ctx, sessionID)
	if err != nil {
		return err
	}
	classification := store.ClassificationThirdParty
	if c.Author.IsBot || c.Author.ID == saved.ActorID || turn.HasMarker(c.Body) {
		classification = store.ClassificationSelf
	} else if authorized {
		classification = store.ClassificationInstruction
	}
	_, err = d.db.RecordTicketReply(ctx, sessionID, *reply, classification)
	return err
}

package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
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
		ids, err := d.projectTicketIdentities(ctx, sessionID)
		if err != nil {
			return turn.Admission{}, err
		}
		filter, err := turn.NewFilter(d.db, ids, d.log)
		if err != nil {
			return turn.Admission{}, err
		}
		if reply == nil {
			return turn.Admission{}, errors.New("qualified project reply is required")
		}
		session, err := d.db.GetSession(ctx, sessionID)
		if err != nil {
			return turn.Admission{}, err
		}
		appointment, err := d.db.CurrentApprover(ctx, session.RepoID)
		if err != nil {
			return turn.Admission{}, err
		}
		verdict := filter.ClassifyReply(*reply)
		if verdict.IsSelf() {
			return filter.AdmitReply(ctx, sessionID, *reply)
		}
		if appointment.RequireFresh && !reply.CreatedAt.After(appointment.ActivatedAt) {
			// Preserve the observation as context, without admitting old authority.
			recorded, err := d.db.RecordTicketReply(ctx, sessionID, *reply, store.ClassificationThirdParty)
			return turn.Admission{Verdict: turn.Verdict{Classification: store.ClassificationThirdParty}, Comment: recorded}, err
		}
		if !verdict.Triggers() {
			return filter.AdmitReply(ctx, sessionID, *reply)
		}
		claim, err := d.db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: appointment.RepoID, AppointmentID: appointment.ID, Revision: appointment.Revision, SessionID: sessionID, Actor: reply.Author.Identity, Source: reply.Identity, CreatedAt: reply.CreatedAt, Kind: "instruction", Key: fmt.Sprintf("%d:%s", sessionID, reply.Identity.RemoteID)})
		if err != nil {
			return turn.Admission{}, err
		}
		admitted, err := filter.AdmitReply(ctx, sessionID, *reply)
		return admitted, errors.Join(err, d.db.CompleteProjectAuthority(context.WithoutCancel(ctx), claim.ID))
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
	session, err := d.db.GetSession(ctx, sessionID)
	if err != nil {
		return turn.Identities{}, err
	}
	appointment, err := d.db.CurrentApprover(ctx, session.RepoID)
	if err != nil {
		return turn.Identities{}, fmt.Errorf("project ticket approver is unavailable; inspect repo approver: %w", err)
	}
	binding, err := d.db.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return turn.Identities{}, err
	}
	if appointment.Actor.Provider != binding.Locator.Provider || appointment.Actor.Origin != binding.Locator.Origin {
		return turn.Identities{}, fmt.Errorf("%w: project approver origin differs from saved ticket", store.ErrApproverUnauthorized)
	}
	owner, err := strconv.ParseInt(appointment.Actor.RemoteID, 10, 64)
	if appointment.Actor.Provider == ticket.Linear {
		owner, err = d.db.LocalTicketSlot(ctx, "actor", appointment.Actor)
	}
	if err != nil || owner <= 0 {
		return turn.Identities{}, errors.New("invalid project approver identity")
	}
	saved, err := d.db.GetExecutionSnapshot(ctx, sessionID)
	if err != nil {
		return turn.Identities{}, err
	}
	ids := turn.Identities{OwnerUserID: owner, OwnerHandle: appointment.Handle, BotUserID: saved.ActorID, BotHandle: saved.ActorLogin, TicketOrigin: appointment.Actor.Origin, OwnerIdentity: appointment.Actor}
	if appointment.Actor.Provider == ticket.Linear {
		ids.BotIdentity, err = d.db.TicketIdentityForSlot(ctx, "actor", saved.ActorID)
		if err != nil {
			return turn.Identities{}, err
		}
	}
	return ids, ids.Validate()
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
	ids, err := d.projectTicketIdentities(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if c.Author.ID != ids.OwnerUserID || c.Author.IsBot || turn.HasMarker(c.Body) {
		return false, nil
	}
	session, err := d.db.GetSession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	appointment, err := d.db.CurrentApprover(ctx, session.RepoID)
	if err != nil {
		return false, err
	}
	if appointment.RequireFresh && !c.CreatedAt.After(appointment.ActivatedAt) {
		return false, nil
	}
	source := ticket.Identity{Provider: appointment.Actor.Provider, Origin: appointment.Actor.Origin, RemoteID: strconv.FormatInt(c.ID, 10)}
	if c.QualifiedReply != nil {
		source = c.QualifiedReply.Identity
	}
	claim, err := d.db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: session.RepoID, AppointmentID: appointment.ID, Revision: appointment.Revision, Actor: appointment.Actor, Source: source, CreatedAt: c.CreatedAt, Kind: "child-instruction", Key: source.RemoteID})
	if err != nil {
		return false, err
	}
	return true, d.db.CompleteProjectAuthority(ctx, claim.ID)
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

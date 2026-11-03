package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TicketAdmission owns current project authority, activation boundaries and
// durable instruction claims. The host supplies verified provider observations.
type TicketAdmission struct {
	db  *store.DB
	log *slog.Logger
}

// NewTicketAdmission binds the private persistence and classification adapters.
func NewTicketAdmission(db *store.DB, log *slog.Logger) *TicketAdmission {
	return &TicketAdmission{db: db, log: log}
}

// Admit applies current project authority before admitting a qualified reply.
func (a *TicketAdmission) Admit(ctx context.Context, sessionID int64, reply *ticket.Reply) (turn.Admission, error) {
	ids, err := a.Identities(ctx, sessionID)
	if err != nil {
		return turn.Admission{}, err
	}
	filter, err := turn.NewFilter(a.db, ids, a.log)
	if err != nil {
		return turn.Admission{}, err
	}
	if reply == nil {
		return turn.Admission{}, errors.New("qualified project reply is required")
	}
	session, err := a.db.GetSession(ctx, sessionID)
	if err != nil {
		return turn.Admission{}, err
	}
	appointment, err := a.db.CurrentApprover(ctx, session.RepoID)
	if err != nil {
		return turn.Admission{}, err
	}
	verdict := filter.ClassifyReply(*reply)
	if verdict.IsSelf() {
		return filter.AdmitReply(ctx, sessionID, *reply)
	}
	if appointment.RequireFresh && !reply.CreatedAt.After(appointment.ActivatedAt) {
		// Preserve the observation as context, without admitting old authority.
		recorded, err := a.db.RecordTicketReply(ctx, sessionID, *reply, store.ClassificationThirdParty)
		return turn.Admission{Verdict: turn.Verdict{Classification: store.ClassificationThirdParty}, Comment: recorded}, err
	}
	if !verdict.Triggers() {
		return filter.AdmitReply(ctx, sessionID, *reply)
	}
	claim, err := a.db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: appointment.RepoID, AppointmentID: appointment.ID, Revision: appointment.Revision, SessionID: sessionID, Actor: reply.Author.Identity, Source: reply.Identity, CreatedAt: reply.CreatedAt, Kind: "instruction", Key: fmt.Sprintf("%d:%s", sessionID, reply.Identity.RemoteID)})
	if err != nil {
		return turn.Admission{}, err
	}
	admitted, err := filter.AdmitReply(ctx, sessionID, *reply)
	return admitted, errors.Join(err, a.db.CompleteProjectAuthority(context.WithoutCancel(ctx), claim.ID))
}

// Identities resolves current ticket approval authority against the retained route.
func (a *TicketAdmission) Identities(ctx context.Context, sessionID int64) (turn.Identities, error) {
	session, err := a.db.GetSession(ctx, sessionID)
	if err != nil {
		return turn.Identities{}, err
	}
	appointment, err := a.db.CurrentApprover(ctx, session.RepoID)
	if err != nil {
		return turn.Identities{}, fmt.Errorf("project ticket approver is unavailable; inspect repo approver: %w", err)
	}
	binding, err := a.db.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return turn.Identities{}, err
	}
	if appointment.Actor.Provider != binding.Locator.Provider || appointment.Actor.Origin != binding.Locator.Origin {
		return turn.Identities{}, fmt.Errorf("%w: project approver origin differs from saved ticket", store.ErrApproverUnauthorized)
	}
	owner, err := strconv.ParseInt(appointment.Actor.RemoteID, 10, 64)
	if appointment.Actor.Provider == ticket.Linear {
		owner, err = a.db.LocalTicketSlot(ctx, "actor", appointment.Actor)
	}
	if err != nil || owner <= 0 {
		return turn.Identities{}, errors.New("invalid project approver identity")
	}
	saved, err := a.db.GetExecutionSnapshot(ctx, sessionID)
	if err != nil {
		return turn.Identities{}, err
	}
	ids := turn.Identities{OwnerUserID: owner, OwnerHandle: appointment.Handle, BotUserID: saved.ActorID, BotHandle: saved.ActorLogin, TicketOrigin: appointment.Actor.Origin, OwnerIdentity: appointment.Actor}
	if appointment.Actor.Provider == ticket.Linear {
		ids.BotIdentity, err = a.db.TicketIdentityForSlot(ctx, "actor", saved.ActorID)
		if err != nil {
			return turn.Identities{}, err
		}
	}
	return ids, ids.Validate()
}

// EligibleComment applies current approval and activation fences to managed-story input.
func (a *TicketAdmission) EligibleComment(ctx context.Context, sessionID int64, c github.Comment) (bool, error) {
	ids, err := a.Identities(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if c.Author.ID != ids.OwnerUserID || c.Author.IsBot || turn.HasMarker(c.Body) {
		return false, nil
	}
	session, err := a.db.GetSession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	appointment, err := a.db.CurrentApprover(ctx, session.RepoID)
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
	claim, err := a.db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: session.RepoID, AppointmentID: appointment.ID, Revision: appointment.Revision, Actor: appointment.Actor, Source: source, CreatedAt: c.CreatedAt, Kind: "child-instruction", Key: source.RemoteID})
	if err != nil {
		return false, err
	}
	return true, a.db.CompleteProjectAuthority(ctx, claim.ID)
}

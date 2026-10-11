package projectconnection

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

var (
	// ErrApproverNotHuman rejects deleted, bot, organization and unknown account types.
	ErrApproverNotHuman = errors.New("ticket approver must be an existing human account with an immutable identity")
	// ErrApproverPublisher prevents the factory from treating its own account as authority.
	ErrApproverPublisher = errors.New("ticket approver must differ from the publishing account")
)

// ApproverService exposes trusted local project management. Ticket and agent
// transports deliberately do not expose these mutation operations.
type ApproverService struct {
	DB      *store.DB
	Factory *Factory
}

// ApproverInspection describes authority without credentials.
type ApproverInspection struct {
	Appointment    store.ApproverAppointment
	Connection     store.ConnectionVersion
	Blockers       []store.ApproverBlocker
	Initialization store.ApproverInitialization
}

// Inspect returns the retained appointment, configuration, and actionable blockers.
func (s ApproverService) Inspect(ctx context.Context, projectID int64) (ApproverInspection, error) {
	var out ApproverInspection
	var err error
	out.Connection, err = s.DB.CurrentConnection(ctx, projectID, store.TicketConnection)
	if err != nil {
		return out, err
	}
	out.Blockers, err = s.DB.ApproverBlockers(ctx, projectID)
	if err != nil {
		return out, err
	}
	out.Initialization, err = s.DB.GetApproverInitialization(ctx, projectID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return out, err
	}
	out.Appointment, err = s.DB.RawCurrentApprover(ctx, projectID)
	return out, err
}

// Set verifies the requested handle against the configured origin before the
// store compares both observed revisions and atomically admits the appointment.
func (s ApproverService) Set(ctx context.Context, projectID int64, login string, expectedConnectionRevision, expectedAuthorityRevision int64) (store.ApproverAppointment, error) {
	login = strings.TrimSpace(login)
	if login == "" || strings.ContainsAny(login, "/\\?# \t\r\n") {
		return store.ApproverAppointment{}, ErrApproverNotHuman
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	v, clients, err := s.verificationClient(ctx, projectID, expectedConnectionRevision)
	if err != nil {
		return store.ApproverAppointment{}, err
	}
	user, err := clients.Code.UserByLogin(ctx, login)
	if err != nil {
		return store.ApproverAppointment{}, fmt.Errorf("verify ticket approver: %w", safeFailure(err))
	}
	a, err := verifiedAppointment(v, user, clients.Actor)
	if err != nil {
		return store.ApproverAppointment{}, err
	}
	return s.DB.SetApprover(ctx, a, expectedConnectionRevision, expectedAuthorityRevision)
}

// Refresh resolves the saved immutable account ID, so a renamed or reassigned
// handle cannot appoint another account. The store retains activation and revision.
func (s ApproverService) Refresh(ctx context.Context, projectID, expectedConnectionRevision, expectedAuthorityRevision int64) (store.ApproverAppointment, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	old, err := s.DB.CurrentApprover(ctx, projectID)
	if err != nil {
		return store.ApproverAppointment{}, err
	}
	if old.Revision != expectedAuthorityRevision {
		return store.ApproverAppointment{}, store.ErrApproverConflict
	}
	v, clients, err := s.verificationClient(ctx, projectID, expectedConnectionRevision)
	if err != nil {
		return store.ApproverAppointment{}, err
	}
	if old.Actor.Provider != v.Provider || old.Actor.Origin != v.Origin {
		return store.ApproverAppointment{}, ErrInvalidConnection
	}
	id, err := strconv.ParseInt(old.Actor.RemoteID, 10, 64)
	if err != nil || id <= 0 {
		return store.ApproverAppointment{}, ErrApproverNotHuman
	}
	user, err := clients.Code.UserByID(ctx, id)
	if err != nil {
		return store.ApproverAppointment{}, fmt.Errorf("refresh ticket approver: %w", safeFailure(err))
	}
	a, err := verifiedAppointment(v, user, clients.Actor)
	if err != nil {
		return store.ApproverAppointment{}, err
	}
	if a.Actor != old.Actor {
		return store.ApproverAppointment{}, store.ErrApproverConflict
	}
	a.Provenance = old.Provenance
	return s.DB.SetApprover(ctx, a, expectedConnectionRevision, expectedAuthorityRevision)
}

func (s ApproverService) verificationClient(ctx context.Context, projectID, revision int64) (store.ConnectionVersion, *Clients, error) {
	v, err := s.DB.CurrentConnection(ctx, projectID, store.TicketConnection)
	if err != nil {
		return v, nil, err
	}
	if v.Revision != revision {
		return v, nil, store.ErrConnectionConflict
	}
	if !v.Enabled || v.Provider != ticket.GitHub {
		return v, nil, ErrInvalidConnection
	}
	clients, err := s.Factory.Clients(ctx, v, 0)
	return v, clients, err
}

func verifiedAppointment(v store.ConnectionVersion, user, publisher github.User) (store.ApproverAppointment, error) {
	if user.ID <= 0 || user.Login == "" || user.IsBot || !user.IsHuman || user.Login == "ghost" {
		return store.ApproverAppointment{}, ErrApproverNotHuman
	}
	if user.ID == publisher.ID {
		return store.ApproverAppointment{}, ErrApproverPublisher
	}
	return store.ApproverAppointment{RepoID: v.RepoID, Actor: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: strconv.FormatInt(user.ID, 10)}, Handle: user.Login, VerifiedAt: time.Now().UTC(), Provenance: "explicit"}, nil
}

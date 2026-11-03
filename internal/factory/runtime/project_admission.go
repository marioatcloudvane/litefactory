package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// AdmitProjectTicket verifies trusted saved destinations before atomic admission.
// Existing work always reuses its original snapshot, including after disable.
func AdmitProjectTicket(ctx context.Context, db *store.DB, factory *projectconnection.Factory, repo store.Repo, ref ticket.Ref) (store.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ref.Validate(); err != nil {
		return store.Session{}, err
	}
	if existing, err := db.GetSessionByTicket(ctx, ref.Identity); err == nil {
		if existing.RepoID != repo.ID {
			return store.Session{}, store.ErrTicketIdentityConflict
		}
		binding, bindingErr := db.GetSessionTicket(ctx, existing.ID)
		if bindingErr != nil {
			return store.Session{}, bindingErr
		}
		retainedRef, bindingErr := binding.Ref()
		if bindingErr != nil || retainedRef != ref {
			return store.Session{}, store.ErrTicketIdentityConflict
		}
		if _, err = db.GetExecutionSnapshot(ctx, existing.ID); err != nil {
			return store.Session{}, err
		}
		return existing, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Session{}, err
	}
	if state, err := db.GetProjectImportState(ctx, repo.ID); err == nil && state.Status == "conflict" {
		return store.Session{}, errors.New("project legacy connection evidence conflicts; correct it before admission")
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Session{}, err
	}
	code, err := db.CurrentConnection(ctx, repo.ID, store.CodeConnection)
	if err != nil {
		return store.Session{}, err
	}
	tickets, err := db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
	if err != nil {
		return store.Session{}, err
	}
	if !code.Enabled || !tickets.Enabled {
		return store.Session{}, store.ErrConnectionDisabled
	}
	if ref.Identity.Provider != tickets.Provider || ref.Identity.Origin != tickets.Origin || ref.Locator.Container != tickets.Destination {
		return store.Session{}, store.ErrTicketIdentityConflict
	}
	ticketClients, err := factory.Clients(ctx, tickets, 0)
	if err != nil {
		return store.Session{}, err
	}
	observed, err := ticketClients.Tickets.Read(ctx, ref)
	if err != nil {
		return store.Session{}, err
	}
	if observed.Ref != ref {
		return store.Session{}, store.ErrTicketIdentityConflict
	}
	codeActor := ticketClients.Actor.ID
	if tickets.Provider == ticket.Linear {
		codeActor = 0
	}
	codeClients, err := factory.Clients(ctx, code, codeActor)
	if err != nil {
		return store.Session{}, err
	}
	owner, name, _ := strings.Cut(code.Destination, "/")
	access, err := codeClients.Code.InspectRepository(ctx, github.Repo{Owner: owner, Name: name})
	if err != nil {
		return store.Session{}, err
	}
	if !strings.EqualFold(access.FullName, code.Destination) || (code.RemoteID != "" && code.RemoteID != strconv.FormatInt(access.ID, 10)) {
		return store.Session{}, errors.New("original code repository identity changed")
	}
	number, err := strconv.Atoi(ref.Locator.Key)
	if ref.Identity.Provider == ticket.Linear {
		slot, e := db.LocalTicketSlot(ctx, "issue", ref.Identity)
		number = int(slot)
		err = e
	}
	if err != nil || number <= 0 {
		return store.Session{}, errors.New("GitHub ticket number is invalid")
	}
	ticketOwner, ticketName, _ := strings.Cut(ref.Locator.Container, "/")
	taskRef := naming.Ref{Forge: string(ref.Identity.Provider), Owner: ticketOwner, Repo: ticketName, Issue: number}.String()
	// A qualified alias is allocated only when the historical alias belongs to
	// another immutable ticket. Frozen historical paths are never renamed.
	if prior, e := db.GetSessionByTaskRef(ctx, taskRef); e == nil {
		priorTicket, e := db.GetSessionTicket(ctx, prior.ID)
		if e != nil {
			return store.Session{}, e
		}
		if priorTicket.RemoteID != ref.Identity.RemoteID || priorTicket.Locator.Origin != ref.Identity.Origin {
			taskRef = naming.QualifiedTaskRef(taskRef, ref.Identity)
		}
	} else if !errors.Is(e, store.ErrNotFound) {
		return store.Session{}, e
	}
	ticketOwnerForRead, ticketNameForRead, _ := strings.Cut(tickets.Destination, "/")
	var ticketAccess github.RepositoryAccess
	if tickets.Provider == ticket.Linear {
		identity, e := ticketClients.Tickets.ContainerIdentity(ctx, tickets.Destination)
		if e != nil {
			return store.Session{}, e
		}
		if identity.Provider != tickets.Provider || identity.Origin != tickets.Origin || identity.RemoteID != tickets.Linear.ProjectID || (tickets.RemoteID != "" && identity.RemoteID != tickets.RemoteID) {
			return store.Session{}, store.ErrTicketIdentityConflict
		}
		ticketAccess.FullName = tickets.Destination
	} else {
		ticketAccess, err = ticketClients.Code.InspectRepository(ctx, github.Repo{Owner: ticketOwnerForRead, Name: ticketNameForRead})
	}
	if err != nil {
		return store.Session{}, err
	}
	if tickets.Provider == ticket.GitHub && (!strings.EqualFold(ticketAccess.FullName, tickets.Destination) || (tickets.RemoteID != "" && tickets.RemoteID != strconv.FormatInt(ticketAccess.ID, 10))) {
		return store.Session{}, store.ErrTicketIdentityConflict
	}
	appointment, err := db.CurrentApprover(ctx, repo.ID)
	if err != nil {
		return store.Session{}, err
	}
	ticketRemote := strconv.FormatInt(ticketAccess.ID, 10)
	if tickets.Provider == ticket.Linear {
		ticketRemote = tickets.Linear.ProjectID
	}
	var session store.Session
	if tickets.Provider == ticket.Linear {
		session, err = db.AdmitSessionWithPublishers(ctx, repo, ref, code.Revision, tickets.Revision, store.PublishingActor{ID: ticketClients.Actor.ID, Login: ticketClients.Actor.Login}, store.PublishingActor{ID: codeClients.Actor.ID, Login: codeClients.Actor.Login}, taskRef, appointment.Revision, strconv.FormatInt(access.ID, 10), ticketRemote)
	} else {
		session, err = db.AdmitSessionWithApprover(ctx, repo, ref, code.Revision, tickets.Revision, ticketClients.Actor.ID, ticketClients.Actor.Login, taskRef, appointment.Revision, strconv.FormatInt(access.ID, 10), ticketRemote)
	}
	if err != nil {
		return store.Session{}, fmt.Errorf("admit project ticket: %w", err)
	}
	return session, nil
}

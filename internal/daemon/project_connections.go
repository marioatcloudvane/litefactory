package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
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

type connectionScopeKey struct{}
type connectionScope struct{ projectID, sessionID int64 }

func projectContext(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, connectionScopeKey{}, connectionScope{projectID: id})
}
func sessionContext(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, connectionScopeKey{}, connectionScope{sessionID: id})
}

// selectTicketConnection is shared by ingress, workflow effects and recovery.
// A saved session wins over mutable current settings, even when disabled.
func selectTicketConnection(ctx context.Context, db *store.DB, factory *projectconnection.Factory, repo github.Repo, number int, sessionID int64) (projectconnection.TicketClient, int64, error) {
	scope, _ := ctx.Value(connectionScopeKey{}).(connectionScope)
	doctorSource, doctorBound := ctx.Value(doctorSourceContextKey{}).(store.DoctorCommandSource)
	if scoped := turn.ExecutionSessionID(ctx); scoped > 0 {
		scope.sessionID = scoped
		scope.projectID = 0
	}
	if sessionID == 0 {
		sessionID = scope.sessionID
	}
	if sessionID == 0 && number > 0 && scope.projectID == 0 && !doctorBound {
		projects, e := db.ListRepos(ctx)
		if e != nil {
			return nil, 0, e
		}
		for _, p := range projects {
			sessions, e := db.ListRepoSessions(ctx, p.ID)
			if e != nil {
				return nil, 0, e
			}
			for _, s := range sessions {
				if s.IssueNumber != number {
					continue
				}
				binding, e := db.GetSessionTicket(ctx, s.ID)
				if e != nil {
					if errors.Is(e, store.ErrNotFound) {
						continue
					}
					return nil, 0, e
				}
				if binding.Locator.Container != repo.String() {
					continue
				}
				if sessionID != 0 && sessionID != s.ID {
					return nil, 0, store.ErrTicketIdentityConflict
				}
				sessionID = s.ID
			}
		}
	}

	var version store.ConnectionVersion
	var actor int64
	var retainedRemote string
	var err error
	switch {
	case doctorBound:
		version, err = db.GetConnectionVersion(ctx, doctorSource.ConnectionVersionID)
		actor = doctorSource.PublisherID
		retainedRemote = version.RemoteID
		if err == nil && (version.Provider != doctorSource.Ticket.Identity.Provider || version.Origin != doctorSource.Ticket.Identity.Origin || version.Destination != doctorSource.Ticket.Locator.Container) {
			return nil, 0, store.ErrTicketIdentityConflict
		}
	case sessionID > 0:
		snapshot, e := db.GetExecutionSnapshot(ctx, sessionID)
		if e != nil {
			return nil, 0, e
		}
		actor = snapshot.ActorID
		retainedRemote = snapshot.TicketRemoteID
		version, err = db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
	case scope.projectID > 0:
		version, err = db.CurrentConnection(ctx, scope.projectID, store.TicketConnection)
		if err == nil && !version.Enabled {
			return nil, 0, store.ErrConnectionDisabled
		}
	default:
		// Container-only recovery must be unambiguous; it never chooses a new
		// project version when retained sessions name more than one origin.
		projects, e := db.ListRepos(ctx)
		if e != nil {
			return nil, 0, e
		}
		var selected bool
		for _, p := range projects {
			sessions, e := db.ListRepoSessions(ctx, p.ID)
			if e != nil {
				return nil, 0, e
			}
			for _, s := range sessions {
				binding, e := db.GetSessionTicket(ctx, s.ID)
				if e != nil {
					if errors.Is(e, store.ErrNotFound) {
						continue
					}
					return nil, 0, e
				}
				if binding.Locator.Container != repo.String() {
					continue
				}
				snapshot, e := db.GetExecutionSnapshot(ctx, s.ID)
				if e != nil {
					return nil, 0, e
				}
				v, e := db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
				if e != nil {
					return nil, 0, e
				}
				if selected && (version.ID != v.ID || actor != snapshot.ActorID) {
					return nil, 0, store.ErrTicketIdentityConflict
				}
				version, actor, selected = v, snapshot.ActorID, true
				retainedRemote = snapshot.TicketRemoteID
			}
		}
		if !selected {
			return nil, 0, store.ErrNotFound
		}
	}
	if err != nil {
		return nil, 0, err
	}
	if repo.Owner != "" && version.Destination != repo.String() {
		return nil, 0, store.ErrTicketIdentityConflict
	}
	clients, err := factory.Clients(ctx, version, actor)
	if err != nil {
		return nil, 0, err
	}
	if retainedRemote == "" {
		retainedRemote = version.RemoteID
	}
	if actor > 0 {
		if retainedRemote == "" {
			return nil, 0, errors.New("original ticket container identity is unresolved")
		}
		identity, checkErr := tickContainerIdentity(ctx, clients.Tickets, version.ID, actor, version.Destination)
		if checkErr != nil {
			return nil, 0, checkErr
		}
		if retainedRemote != identity.RemoteID {
			return nil, 0, errors.New("original ticket container identity changed")
		}
	}
	return clients.Tickets, clients.Actor.ID, nil
}

// containerCheckKey carries one poll cycle's original-container observations.
type containerCheckKey struct{}

// containerCheckEntry is one key's inspection. done closes once identity and
// err are settled; until then later callers for the key wait on it.
type containerCheckEntry struct {
	done     chan struct{}
	identity ticket.Identity
	err      error
}

// containerChecks memoizes ContainerIdentity per connection version and actor
// for one scope (a tick, or a worker's observation pass), so N task reads in
// the scope cost one repository inspection. The lock never spans I/O:
// concurrent callers for one key share the in-flight inspection and other keys
// proceed independently. After close, callers fall back to a fresh check.
type containerChecks struct {
	mu      sync.Mutex
	closed  bool
	entries map[[2]int64]*containerCheckEntry
}

func withContainerChecks(ctx context.Context) (context.Context, *containerChecks) {
	m := &containerChecks{entries: make(map[[2]int64]*containerCheckEntry)}
	return context.WithValue(ctx, containerCheckKey{}, m), m
}

// withoutContainerChecks hides any enclosing scope so later checks are fresh.
func withoutContainerChecks(ctx context.Context) context.Context {
	return context.WithValue(ctx, containerCheckKey{}, (*containerChecks)(nil))
}

func (m *containerChecks) close() {
	m.mu.Lock()
	m.closed = true
	m.entries = nil
	m.mu.Unlock()
}

// cachedContainerIdentity returns a settled observation from the live scope
// without performing I/O; hit is false when none is available. A hit may carry
// the scope's cached error.
func cachedContainerIdentity(ctx context.Context, versionID, actor int64) (identity ticket.Identity, hit bool, err error) {
	m, _ := ctx.Value(containerCheckKey{}).(*containerChecks)
	if m == nil {
		return ticket.Identity{}, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ticket.Identity{}, false, nil
	}
	e, ok := m.entries[[2]int64{versionID, actor}]
	if !ok {
		return ticket.Identity{}, false, nil
	}
	select {
	case <-e.done:
		return e.identity, true, e.err
	default:
		return ticket.Identity{}, false, nil
	}
}

func tickContainerIdentity(ctx context.Context, tickets projectconnection.TicketClient, versionID, actor int64, destination string) (ticket.Identity, error) {
	m, _ := ctx.Value(containerCheckKey{}).(*containerChecks)
	if m == nil {
		return tickets.ContainerIdentity(ctx, destination)
	}
	key := [2]int64{versionID, actor}
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return tickets.ContainerIdentity(ctx, destination)
		}
		e, ok := m.entries[key]
		if !ok {
			e = &containerCheckEntry{done: make(chan struct{})}
			m.entries[key] = e
			m.mu.Unlock()
			identity, err := tickets.ContainerIdentity(ctx, destination)
			m.mu.Lock()
			if err != nil && ctx.Err() != nil {
				// A caller's own cancellation is not an observation for the
				// scope; waiters retry with their own context.
				if m.entries[key] == e {
					delete(m.entries, key)
				}
			} else {
				e.identity, e.err = identity, err
			}
			close(e.done)
			m.mu.Unlock()
			return identity, err
		}
		m.mu.Unlock()
		select {
		case <-e.done:
		case <-ctx.Done():
			return ticket.Identity{}, ctx.Err()
		}
		m.mu.Lock()
		settled := m.entries[key] == e
		m.mu.Unlock()
		if settled {
			return e.identity, e.err
		}
		// The leader was cancelled (or the scope closed); try again.
	}
}

func (d *Daemon) connectionIngress(ctx context.Context, repo github.Repo, number int) (TicketIngress, ticket.Origin, error) {
	if d.connections == nil {
		if d.tickets == nil {
			return nil, "", nil
		}
		if d.filter == nil {
			return nil, "", errors.New("ticket ingress requires configured identities")
		}
		return d.tickets, d.filter.Identities().TicketOrigin, nil
	}
	selected, _, err := selectTicketConnection(ctx, d.db, d.connections, repo, number, 0)
	if err != nil {
		return nil, "", err
	}
	return selected, selected.Origin(), nil
}

// ProjectTicketSelector binds local management and recovery to the same retained
// execution selection used by daemon workers. No mutable replacement fallback exists.
func ProjectTicketSelector(db *store.DB, factory *projectconnection.Factory) turn.TicketConnectionSelector {
	return func(ctx context.Context, repo github.Repo, number int, sessionID int64) (turn.WorkflowTickets, int64, error) {
		adapter, actor, err := selectTicketConnection(ctx, db, factory, repo, number, sessionID)
		if ctx.Err() == nil && errors.Is(err, projectconnection.ErrActorChanged) {
			return nil, 0, fmt.Errorf("%w: %w", turn.ErrRequiredTicketPublisher, err)
		}
		return adapter, actor, err
	}
}

func (d *Daemon) sameTicketSource(ctx context.Context, repoID int64, parent string) (bool, error) {
	if d.connections == nil {
		return true, nil
	}
	session, err := d.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return false, err
	}
	snapshot, err := d.db.GetExecutionSnapshot(ctx, session.ID)
	if err != nil {
		return false, err
	}
	historical, err := d.db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
	if err != nil {
		return false, err
	}
	current, err := d.db.CurrentConnection(ctx, repoID, store.TicketConnection)
	if err != nil {
		return false, err
	}
	return historical.Provider == current.Provider && historical.Origin == current.Origin && historical.Destination == current.Destination, nil
}

// sameSessionTicketSource prevents number-only managed registries from claiming
// a ticket from another source after a project connection changes.
func (d *Daemon) sameSessionTicketSource(ctx context.Context, sessionID int64, parent string) (bool, error) {
	if d.connections == nil {
		return true, nil
	}
	parentSession, err := d.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return false, err
	}
	child, err := d.db.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return false, err
	}
	original, err := d.db.GetSessionTicket(ctx, parentSession.ID)
	if err != nil {
		return false, err
	}
	return child.Locator.Provider == original.Locator.Provider && child.Locator.Origin == original.Locator.Origin && child.Locator.Container == original.Locator.Container, nil
}

func (d *Daemon) graphPlanStoryForTask(ctx context.Context, t *task) (store.GraphPlanStory, bool, error) {
	story, owned, err := d.db.GraphPlanStoryAt(ctx, t.repo.ID, t.issue.Number)
	if err != nil || !owned {
		return story, owned, err
	}
	same, err := d.sameSessionTicketSource(ctx, t.session.ID, story.Parent)
	if err != nil || !same {
		return store.GraphPlanStory{}, false, err
	}
	if d.connections != nil && story.IssueID != t.issue.ID {
		return store.GraphPlanStory{}, false, nil
	}
	return story, true, nil
}

func (d *Daemon) graphPlanParentForTask(ctx context.Context, t *task) (string, error) {
	parent, err := d.db.GraphPlanParentForChild(ctx, t.repo.ID, t.issue.Number)
	if err != nil {
		return "", err
	}
	same, err := d.sameSessionTicketSource(ctx, t.session.ID, parent)
	if err != nil {
		return "", err
	}
	if !same {
		return "", store.ErrNotFound
	}
	return parent, nil
}

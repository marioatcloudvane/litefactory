package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// managedChildSession resolves a child using the admitted parent's original
// ticket client and actor. Disabled current settings apply only to new roots.
func (d *Daemon) managedChildSession(ctx context.Context, repo store.Repo, parentTask string, number int, remoteID int64) (store.Session, error) {
	if d.connections == nil {
		return d.db.GetOrCreateSession(ctx, repo, number)
	}
	parent, err := d.db.GetSessionByTaskRef(ctx, parentTask)
	if err != nil {
		return store.Session{}, err
	}
	snapshot, err := d.db.GetExecutionSnapshot(ctx, parent.ID)
	if err != nil {
		return store.Session{}, err
	}
	if snapshot.RepoID != repo.ID {
		return store.Session{}, store.ErrBindingConflict
	}
	version, err := d.db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
	if err != nil {
		return store.Session{}, err
	}
	// One bounded read verifies the child ID without probing through replacement
	// credentials or performing ticket effects.
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	clients, err := d.connections.Clients(checkCtx, version, snapshot.ActorID)
	if err != nil {
		return store.Session{}, err
	}
	owner, name, _ := strings.Cut(version.Destination, "/")
	container, err := tickContainerIdentity(checkCtx, clients.Tickets, version.ID, snapshot.ActorID, version.Destination)
	if err != nil {
		return store.Session{}, err
	}
	remoteContainer := snapshot.TicketRemoteID
	if remoteContainer == "" {
		remoteContainer = version.RemoteID
	}
	if remoteContainer == "" || remoteContainer != container.RemoteID {
		return store.Session{}, errors.New("managed child original ticket container identity changed or unresolved")
	}
	key := strconv.Itoa(number)
	if version.Provider == ticket.Linear {
		id, e := d.db.TicketIdentityForSlot(ctx, "issue", int64(number))
		if e != nil {
			return store.Session{}, e
		}
		key = id.RemoteID
	}
	observed, err := clients.Tickets.Resolve(checkCtx, ticket.Locator{Provider: version.Provider, Origin: version.Origin, Container: version.Destination, Key: key})
	if err != nil {
		return store.Session{}, err
	}
	expectedID := strconv.FormatInt(remoteID, 10)
	if remoteID > 0 && version.Provider == ticket.Linear {
		id, e := d.db.TicketIdentityForSlot(ctx, "issue", remoteID)
		if e != nil {
			return store.Session{}, e
		}
		expectedID = id.RemoteID
	}
	if remoteID > 0 && observed.Ref.Identity.RemoteID != expectedID {
		return store.Session{}, errors.New("managed child original ticket identity changed")
	}
	alias := naming.Ref{Forge: string(version.Provider), Owner: owner, Repo: name, Issue: number}.String()
	return d.db.AdmitManagedChild(ctx, parent.ID, observed.Ref, alias)
}

// knownManagedChildSession finds a child already admitted under the parent from
// local state only, so a story with nothing new to do costs no ticket read. It
// reports false when the child must still be resolved and admitted remotely.
func (d *Daemon) knownManagedChildSession(ctx context.Context, repo store.Repo, parentTask string, remoteID int64) (store.Session, bool, error) {
	if d.connections == nil || remoteID <= 0 {
		return store.Session{}, false, nil
	}
	parent, err := d.db.GetSessionByTaskRef(ctx, parentTask)
	if err != nil {
		return store.Session{}, false, err
	}
	snapshot, err := d.db.GetExecutionSnapshot(ctx, parent.ID)
	if err != nil {
		return store.Session{}, false, err
	}
	if snapshot.RepoID != repo.ID {
		return store.Session{}, false, store.ErrBindingConflict
	}
	version, err := d.db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
	if err != nil {
		return store.Session{}, false, err
	}
	expectedID := strconv.FormatInt(remoteID, 10)
	if version.Provider == ticket.Linear {
		id, e := d.db.TicketIdentityForSlot(ctx, "issue", remoteID)
		if e != nil {
			return store.Session{}, false, e
		}
		expectedID = id.RemoteID
	}
	child, err := d.db.GetSessionByTicket(ctx, ticket.Identity{Provider: version.Provider, Origin: version.Origin, RemoteID: expectedID})
	if errors.Is(err, store.ErrNotFound) {
		return store.Session{}, false, nil
	}
	if err != nil {
		return store.Session{}, false, err
	}
	childSnapshot, err := d.db.GetExecutionSnapshot(ctx, child.ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Session{}, false, nil
	}
	if err != nil {
		return store.Session{}, false, err
	}
	// Anything that does not match the parent's admission as AdmitManagedChild
	// requires is left to the remote path, which refuses it with the reason.
	if childSnapshot.CodeVersionID != snapshot.CodeVersionID && childSnapshot.CodeRemoteID == "" {
		return store.Session{}, false, nil
	}
	if childSnapshot.RepoID != snapshot.RepoID || childSnapshot.ActorID != snapshot.ActorID || childSnapshot.CodeActorID != snapshot.CodeActorID ||
		childSnapshot.CodeRemoteID != snapshot.CodeRemoteID || childSnapshot.TicketRemoteID != snapshot.TicketRemoteID {
		return store.Session{}, false, nil
	}
	return child, true, nil
}

// storySession is the task of a managed story: the one already admitted, or one
// resolved remotely and admitted when none exists yet.
func (d *Daemon) storySession(ctx context.Context, repo store.Repo, parentTask string, number int, remoteID int64) (store.Session, error) {
	session, known, err := d.knownManagedChildSession(ctx, repo, parentTask, remoteID)
	if err != nil || known {
		return session, err
	}
	return d.managedChildSession(ctx, repo, parentTask, number, remoteID)
}

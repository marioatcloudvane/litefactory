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
	container, err := clients.Tickets.ContainerIdentity(checkCtx, version.Destination)
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

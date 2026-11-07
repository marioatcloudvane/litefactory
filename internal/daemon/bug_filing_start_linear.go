package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/factory/runtime"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// The Linear binding hand-off's effect seams. Admit resolves, binds and admits
// the filed issue; Bind records the verified identity on the session. Both are
// idempotent, so a crash between them recovers the one session on the next pass.
var (
	bugStartLinearAdmit = (*Daemon).admitLinearBugSession
	bugStartLinearBind  = func(d *Daemon, ctx context.Context, sessionID int64, ref ticket.Ref) error {
		_, err := d.db.BindSessionTicket(ctx, sessionID, ref)
		return err
	}
)

// linearBugSession gives a Linear filing in state labelled its one session. It
// never creates remote work: the issue already exists, and a connection that
// no longer matches the filing is an error the next pass retries.
func (d *Daemon) linearBugSession(ctx context.Context, repo store.Repo, f store.BugFiling) (store.Session, error) {
	session, ref, err := bugStartLinearAdmit(d, ctx, repo, f)
	if err != nil {
		return store.Session{}, err
	}
	if err := bugStartLinearBind(d, ctx, session.ID, ref); err != nil {
		return store.Session{}, fmt.Errorf("bind the Linear ticket to session %d: %w", session.ID, err)
	}
	return session, nil
}

func (d *Daemon) admitLinearBugSession(ctx context.Context, repo store.Repo, f store.BugFiling) (store.Session, ticket.Ref, error) {
	if d.connections == nil {
		return store.Session{}, ticket.Ref{}, errors.New("project connections are not available")
	}
	if f.IssueNodeID == "" {
		return store.Session{}, ticket.Ref{}, errors.New("labelled Linear bug filing has no remote issue")
	}
	ctx = projectContext(ctx, repo.ID)
	v, err := d.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
	if err != nil {
		return store.Session{}, ticket.Ref{}, err
	}
	if !v.Enabled || v.Provider != ticket.Linear || v.Destination != f.Destination {
		return store.Session{}, ticket.Ref{}, store.ErrConnectionConflict
	}
	clients, err := d.connections.Clients(ctx, v, 0)
	if err != nil {
		return store.Session{}, ticket.Ref{}, err
	}
	snapshot, err := clients.Tickets.Resolve(ctx, ticket.Locator{Provider: ticket.Linear, Origin: v.Origin, Container: v.Destination, Key: f.IssueNodeID})
	if err != nil {
		return store.Session{}, ticket.Ref{}, err
	}
	ref := snapshot.Ref
	if ref.Identity.Provider != v.Provider || ref.Identity.Origin != v.Origin || ref.Locator.Container != v.Destination {
		return store.Session{}, ticket.Ref{}, store.ErrConnectionConflict
	}
	binding, err := d.db.GetTicketBinding(ctx, ref.Identity)
	if errors.Is(err, store.ErrNotFound) {
		_, err = d.db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "discovery"})
	} else if err == nil && (binding.RepoID != repo.ID || !binding.Enabled) {
		err = store.ErrBindingConflict
	}
	if err != nil {
		return store.Session{}, ticket.Ref{}, err
	}
	session, err := runtime.AdmitProjectTicket(ctx, d.db, d.connections, repo, ref)
	return session, ref, err
}

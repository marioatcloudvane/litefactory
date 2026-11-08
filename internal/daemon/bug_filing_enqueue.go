package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/cloudvane/litefactory/internal/factory/runtime"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// bugFilingWorkPayload is the instruction of the run that starts the Bug of an
// owner-confirmed filing. The bug itself is the issue; this text only starts it.
const bugFilingWorkPayload = "Work on the bug this issue reports."

// The Linear session seams: admit resolves, binds and admits the filed issue;
// bind records the verified identity on the session. Both are idempotent.
var (
	bugFilingLinearAdmit = (*Daemon).admitLinearBugSession
	bugFilingLinearBind  = func(d *Daemon, ctx context.Context, sessionID int64, ref ticket.Ref) error {
		_, err := d.db.BindSessionTicket(ctx, sessionID, ref)
		return err
	}
)

// stepBugFilingEnqueues hands every labelled filing to the factory's ordinary
// scheduled work, as the owner, once. An owner-confirmed filing starts like an
// owner-filed issue; everything after that is the Bug workflow's own business.
func (d *Daemon) stepBugFilingEnqueues(ctx context.Context) {
	filings, err := d.db.ActiveBugFilings(ctx, 100)
	if err != nil {
		d.log.ErrorContext(ctx, "could not list bug filings to start", slog.Any("error", err))
		return
	}
	for _, f := range filings {
		if ctx.Err() != nil {
			return
		}
		if f.State != store.BugFilingLabelled && f.State != store.BugFilingStartAdmitted {
			continue
		}
		if err := d.enqueueBugFiling(ctx, f); err != nil {
			d.log.ErrorContext(ctx, "bug filing start failed; it is retried on the next pass",
				slog.Int64("filing", f.ID), slog.Any("error", err))
		}
	}
}

// enqueueBugFiling binds the filed issue's session and records its one start.
// The schedule is keyed by the filing, so a repeat pass finds the same row.
func (d *Daemon) enqueueBugFiling(ctx context.Context, f store.BugFiling) error {
	if f.IssueNumber < 1 {
		return errors.New("labelled bug filing has no issue")
	}
	repo, err := d.db.GetRepo(ctx, f.RepositoryID)
	if err != nil {
		return err
	}
	session, err := d.bugFilingSession(ctx, repo, f)
	if err != nil {
		return err
	}
	now := d.brainstormNow().UTC()
	if _, _, err = d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: planWorkID("bug-filing", strconv.FormatInt(f.ID, 10)),
		OwnerHandle: d.ticketIdentities(sessionContext(ctx, session.ID)).OwnerHandle, Payload: bugFilingWorkPayload, DueAt: now,
	}); err != nil {
		return err
	}
	next := f
	next.SessionID = session.ID
	next.State = store.BugFilingDispatched
	next.Reason = ""
	_, err = d.db.SaveBugFiling(ctx, next, f.State, now)
	return err
}

func (d *Daemon) bugFilingSession(ctx context.Context, repo store.Repo, f store.BugFiling) (store.Session, error) {
	if f.SessionID > 0 {
		return d.db.GetSession(ctx, f.SessionID)
	}
	if f.Tracker != store.BugTrackerLinear {
		return d.db.GetOrCreateSession(ctx, repo, f.IssueNumber)
	}
	session, ref, err := bugFilingLinearAdmit(d, ctx, repo, f)
	if err != nil {
		return store.Session{}, err
	}
	if err := bugFilingLinearBind(d, ctx, session.ID, ref); err != nil {
		return store.Session{}, fmt.Errorf("bind the Linear ticket to session %d: %w", session.ID, err)
	}
	return session, nil
}

// admitLinearBugSession resolves the filed Linear issue and admits its one
// project ticket session. It never creates remote work.
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

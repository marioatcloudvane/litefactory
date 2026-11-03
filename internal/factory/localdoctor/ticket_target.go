package localdoctor

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"strconv"
	"strings"
)

// TicketTarget is trusted core routing evidence, never a conversation view.
type TicketTarget struct {
	Ref         naming.Ref
	Session     store.Session
	Connection  store.ConnectionVersion
	Locator     ticket.Locator
	PublisherID int64
}

// ResolveTicketTarget retains a task's captured connection and publishing actor.
func ResolveTicketTarget(ctx context.Context, db *store.DB, repo store.Repo, issue github.Issue, sessionID int64) (TicketTarget, error) {
	t := TicketTarget{Ref: naming.Ref{Forge: naming.ForgeGitHub, Owner: strings.ToLower(repo.Owner), Repo: strings.ToLower(repo.Name), Issue: issue.Number}}
	var err error
	if t.Connection, err = db.CurrentConnection(ctx, repo.ID, store.TicketConnection); err != nil {
		return t, err
	}
	t.Locator = ticket.Locator{Provider: t.Connection.Provider, Origin: t.Connection.Origin, Container: t.Connection.Destination, Key: strconv.Itoa(issue.Number)}
	if sessionID != 0 {
		t.Session, err = db.GetSession(ctx, sessionID)
	} else {
		t.Session, err = db.GetSessionByTicketLocator(ctx, t.Locator)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return t, err
	}
	if err == nil {
		if t.Session.RepoID != repo.ID {
			return t, store.ErrTicketIdentityConflict
		}
		binding, readErr := db.GetSessionTicket(ctx, t.Session.ID)
		if readErr != nil {
			return t, readErr
		}
		if binding.RemoteID != strconv.FormatInt(issue.ID, 10) || binding.Locator.Key != strconv.Itoa(issue.Number) {
			return t, store.ErrTicketIdentityConflict
		}
		t.Locator = binding.Locator
		snapshot, readErr := db.GetExecutionSnapshot(ctx, t.Session.ID)
		if readErr != nil {
			return t, readErr
		}
		t.PublisherID = snapshot.ActorID
		t.Connection, readErr = db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
		if readErr != nil {
			return t, readErr
		}
		t.Ref, readErr = naming.ParseRef(t.Session.TaskRef)
		if readErr != nil {
			return t, readErr
		}
	} else {
		owner, name, valid := strings.Cut(t.Locator.Container, "/")
		if !valid {
			return t, errors.New("doctor ticket destination is invalid")
		}
		t.Ref.Owner, t.Ref.Repo = owner, name
	}
	return t, nil
}

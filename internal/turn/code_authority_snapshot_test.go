package turn

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestCodeAuthorityRetainsSavedOriginAfterDefaultEditsAndDisables(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "authority.db"))
	mustProgress(t, err)
	defer func() { mustProgress(t, db.Close()) }()
	mustProgress(t, db.Migrate(t.Context()))
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/retained-code", Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: "git@github.com:owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/retained-trees"})
	mustProgress(t, err)
	code, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	mustProgress(t, err)
	tickets, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/tasks", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	mustProgress(t, err)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: tickets.Origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: tickets.Origin, Container: tickets.Destination, Key: "7"}}
	_, err = db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	mustProgress(t, err)
	session, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 99, "original-publisher", "github:owner/tasks#7")
	mustProgress(t, err)
	_, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "code-owner", Source: store.SourceGH})
	mustProgress(t, err)
	// The operator can change future defaults while this execution retains its
	// original publishing account and qualified code and ticket destinations.
	code.Origin = "https://enterprise.example/api/v3"
	code, err = db.SetConnection(t.Context(), code, code.Revision)
	mustProgress(t, err)
	_, err = db.DisableConnection(t.Context(), repo.ID, store.CodeConnection, code.Revision)
	mustProgress(t, err)
	tickets.Origin = code.Origin
	tickets, err = db.SetConnection(t.Context(), tickets, tickets.Revision)
	mustProgress(t, err)
	_, err = db.DisableConnection(t.Context(), repo.ID, store.TicketConnection, tickets.Revision)
	mustProgress(t, err)
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "42"}
	source := ticket.Identity{Provider: ticket.GitHub, Origin: actor.Origin, RemoteID: "101"}
	trigger := Trigger{Actor: actor, Source: source, AuthorID: 42, CommentID: 101, CreatedAt: time.Now(), Body: "/validate-push exact-revision"}
	opts := Options{DB: db, ProjectApprovers: true}
	mustProgress(t, verifyTurnCodeAuthority(t.Context(), opts, session, trigger))
	claim, err := db.ClaimProjectCodeAuthority(t.Context(), store.AuthorityClaim{SessionID: session.ID, RepoID: repo.ID, Actor: actor, Source: source, CreatedAt: trigger.CreatedAt, Kind: "code-control", Key: "saved:101"})
	mustProgress(t, err)
	mustProgress(t, db.CompleteProjectAuthority(t.Context(), claim.ID))
	foreign := trigger
	foreign.Actor.Origin = code.Origin
	if err = verifyTurnCodeAuthority(t.Context(), opts, session, foreign); !errors.Is(err, store.ErrApproverUnauthorized) {
		t.Fatalf("equal ID at current foreign origin authorized: %v", err)
	}
	foreign = trigger
	foreign.Source.Origin = code.Origin
	if err = verifyTurnCodeAuthority(t.Context(), opts, session, foreign); !errors.Is(err, store.ErrApproverUnauthorized) {
		t.Fatalf("foreign source authorized: %v", err)
	}
	_, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 99, Handle: "publisher-renamed", Source: store.SourceGH})
	mustProgress(t, err)
	self := trigger
	self.AuthorID = 99
	self.Actor.RemoteID = "99"
	if err = verifyTurnCodeAuthority(t.Context(), opts, session, self); !errors.Is(err, store.ErrApproverUnauthorized) {
		t.Fatalf("retained publisher gained code authority: %v", err)
	}
	saved, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	mustProgress(t, err)
	if saved.ActorID != 99 || saved.CodeVersionID == code.ID || saved.TicketVersionID == tickets.ID {
		t.Fatalf("current defaults rewrote saved execution: %+v", saved)
	}
}

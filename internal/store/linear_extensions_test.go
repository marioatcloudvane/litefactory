package store

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func linearConnectionFixture(repo Repo) ConnectionVersion {
	v := connectionFixture(repo, TicketConnection)
	v.Provider, v.Origin, v.Destination = "linear", "https://api.linear.app", "team/project"
	v.Linear = LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan","implement":"label-implement"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}
	return v
}

func TestLinearConnectionRetainsMappingsAndQualifiedAuthority(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "code", "repo")
	v, err := db.SetConnection(t.Context(), linearConnectionFixture(repo), 0)
	if err != nil {
		t.Fatal(err)
	}
	changed := v
	changed.Linear.TeamID, changed.Destination = "new-team", "new-team/project"
	if _, err := db.SetConnection(t.Context(), changed, v.Revision); err != nil {
		t.Fatal(err)
	}
	old, err := db.GetConnectionVersion(t.Context(), v.ID)
	if err != nil || old.Linear != v.Linear {
		t.Fatalf("retained configuration changed: %+v %v", old, err)
	}
	changed.Linear.LifecycleStatesJSON = `{"completed":"done","retired":"done"}`
	if _, err := db.SetConnection(t.Context(), changed, v.Revision+1); err == nil {
		t.Fatal("ambiguous lifecycle mapping accepted")
	}
	actor := ticket.Identity{Provider: "linear", Origin: v.Origin, RemoteID: "opaque-human"}
	_, err = db.SetApprover(t.Context(), ApproverAppointment{RepoID: repo.ID, Actor: actor, Handle: "human", VerifiedAt: time.Now(), Provenance: "explicit"}, v.Revision+1, 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTicketLocalSlotsSeparateKindsOriginsAndRetainOriginalIDs(t *testing.T) {
	db := newTestDB(t)
	identity := ticket.Identity{Provider: "linear", Origin: "https://api.linear.app", RemoteID: "opaque"}
	first, err := db.LocalTicketSlot(t.Context(), "issue", identity)
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.LocalTicketSlot(t.Context(), "issue", identity)
	if err != nil || again != first {
		t.Fatalf("unstable slot: %d %d %v", first, again, err)
	}
	comment, err := db.LocalTicketSlot(t.Context(), "comment", identity)
	if err != nil || comment == first {
		t.Fatalf("kinds collapsed: %d %v", comment, err)
	}
	other := identity
	other.Origin = "https://other.example"
	second, err := db.LocalTicketSlot(t.Context(), "issue", other)
	if err != nil || second == first {
		t.Fatalf("origins collapsed: %d %v", second, err)
	}
	recovered, err := db.TicketIdentityForSlot(t.Context(), "issue", first)
	if err != nil || recovered != identity {
		t.Fatalf("lost qualified identity: %+v %v", recovered, err)
	}
	if _, err := db.TicketIdentityForSlot(t.Context(), "actor", first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong kind resolved: %v", err)
	}
}

func TestTicketEffectUUIDAndProviderRepresentationRemainImmutable(t *testing.T) {
	db := newTestDB(t)
	locator := ticket.Locator{Provider: "linear", Origin: "https://api.linear.app", Container: "team/project", Key: "issue"}
	if _, err := db.BindTicketEffect(t.Context(), "effect", locator, ticket.Identity{}); err != nil {
		t.Fatal(err)
	}
	p, err := db.PrepareTicketEffect(t.Context(), "effect", "linear-markdown-v1", "authored", true)
	if err != nil || !creationUUIDPattern.MatchString(p.CreationUUID) {
		t.Fatalf("invalid preparation: %+v %v", p, err)
	}
	again, err := db.PrepareTicketEffect(t.Context(), "effect", "linear-markdown-v1", "authored", true)
	if err != nil || again.CreationUUID != p.CreationUUID {
		t.Fatalf("creation UUID changed: %+v %v", again, err)
	}
	if _, err := db.PrepareTicketEffect(t.Context(), "effect", "linear-markdown-v1", "edited", true); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("prepared bytes replaced: %v", err)
	}
	if err := db.ConfirmTicketEffectRepresentation(t.Context(), "effect", "provider bytes"); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmTicketEffectRepresentation(t.Context(), "effect", "owner edit"); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("provider base replaced: %v", err)
	}
	stored, err := db.GetTicketEffectPreparation(t.Context(), "effect")
	if err != nil || stored.AuthoredContent != "authored" || stored.ObservedContent != "provider bytes" || !stored.Observed {
		t.Fatalf("representation lost: %+v %v", stored, err)
	}
}

func TestLinearAdmissionRetainsPublishingIdentitiesAndReplySources(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "code", "repo")
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	linear, err := db.SetConnection(t.Context(), linearConnectionFixture(repo), 0)
	if err != nil {
		t.Fatal(err)
	}
	identity := ticket.Identity{Provider: "linear", Origin: linear.Origin, RemoteID: "opaque-ticket"}
	ref := ticket.Ref{Identity: identity, Locator: ticket.Locator{Provider: identity.Provider, Origin: identity.Origin, Container: linear.Destination, Key: identity.RemoteID}}
	if _, err := db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: linear.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	bot := ticket.Identity{Provider: "linear", Origin: linear.Origin, RemoteID: "opaque-publisher"}
	botSlot, err := db.LocalTicketSlot(t.Context(), "actor", bot)
	if err != nil {
		t.Fatal(err)
	}
	appointment, err := db.SetApprover(t.Context(), ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.Linear, Origin: linear.Origin, RemoteID: "opaque-owner"}, Handle: "owner", VerifiedAt: time.Now(), Provenance: "explicit"}, linear.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, UserID: 7, Handle: "code-owner", Source: SourceGH})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.AdmitSessionWithPublishers(t.Context(), repo, ref, code.Revision, linear.Revision, PublishingActor{ID: botSlot, Login: "publisher"}, PublishingActor{ID: 42, Login: "github-publisher"}, "linear:team/project#opaque-ticket", appointment.Revision, "123", "project")
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil || admitted.CodeActorID != 42 || admitted.CodeActorLogin != "github-publisher" {
		t.Fatalf("code publisher was not pinned atomically: %+v %v", admitted, err)
	}
	if err := db.PinExecutionCodePublisher(t.Context(), session.ID, 43, "replacement"); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("code publisher replaced: %v", err)
	}
	snapshot, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil || snapshot.ActorID != botSlot || snapshot.CodeActorID != 42 {
		t.Fatalf("publishers collapsed: %+v %v", snapshot, err)
	}
	_, err = db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, UserID: 8, Handle: "new-code-owner", Source: SourceGH})
	if err != nil {
		t.Fatal(err)
	}
	retainedAuthority, err := db.EnsureDeliveryCodeAuthority(t.Context(), session.ID)
	if err != nil || retainedAuthority.OwnerID != 7 {
		t.Fatalf("admission did not retain original code owner: %+v %v", retainedAuthority, err)
	}
	owner := ticket.Actor{Identity: ticket.Identity{Provider: "linear", Origin: linear.Origin, RemoteID: "opaque-owner"}, Handle: "owner"}
	reply := ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: "linear", Origin: linear.Origin, RemoteID: "opaque-reply"}, Author: owner, Body: "/plan work", CreatedAt: time.Now()}
	comment, err := db.RecordTicketReply(t.Context(), session.ID, reply, ClassificationInstruction)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := db.TicketIdentityForSlot(t.Context(), "comment", comment.ForgeCommentID)
	if err != nil || recovered != reply.Identity {
		t.Fatalf("source evidence lost: %+v %v", recovered, err)
	}
	if _, err := db.RecordTicketReply(t.Context(), session.ID, reply, ClassificationInstruction); err != nil {
		t.Fatal(err)
	}
	reply.Author.Identity.RemoteID = "another-owner"
	if _, err := db.RecordTicketReply(t.Context(), session.ID, reply, ClassificationInstruction); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("reply inherited another actor: %v", err)
	}
}

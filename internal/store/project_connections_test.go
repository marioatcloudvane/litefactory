package store

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func connectionFixture(repo Repo, role ConnectionRole) ConnectionVersion {
	return ConnectionVersion{RepoID: repo.ID, Role: role, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TEST_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}
}
func TestProjectConnectionCASImportAndLateObservation(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	v, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	retained := v
	retained.Destination = "old/repo"
	history, err := db.RetainConnectionVersion(t.Context(), retained)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := db.RetainConnectionVersion(t.Context(), retained)
	if err != nil || repeated.ID != history.ID {
		t.Fatalf("historical import not repeatable %+v %v", repeated, err)
	}
	update := v
	update.Destination = "new/code"
	next, err := db.SetConnection(t.Context(), update, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(t.Context(), v, v.Revision); !errors.Is(err, ErrConnectionConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	if err = db.SaveConnectionObservation(t.Context(), ConnectionObservation{VersionID: v.ID, CheckedAt: time.Now(), Status: "usable", Reason: "read_access", Remedy: "none", Capabilities: []string{"read"}}); !errors.Is(err, ErrConnectionConflict) {
		t.Fatalf("late observation: %v", err)
	}
	if err = db.ImportLegacyConnections(t.Context(), connectionFixture(repo, CodeConnection), connectionFixture(repo, TicketConnection)); err != nil {
		t.Fatal(err)
	}
	if err = db.ImportLegacyConnections(t.Context(), connectionFixture(repo, CodeConnection), connectionFixture(repo, TicketConnection)); err != nil {
		t.Fatal(err)
	}
	current, err := db.CurrentConnection(t.Context(), repo.ID, CodeConnection)
	if err != nil || current.ID != next.ID {
		t.Fatalf("explicit replaced: %+v %v", current, err)
	}
}
func TestProjectBindingSnapshotOwnershipAndQualifiedCollision(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	other := testRepo(t, db, "owner", "other")
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "700")
	b, err := db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := db.AddTicketBinding(t.Context(), b)
	if err != nil || same.ID != b.ID {
		t.Fatalf("idempotent add %+v %v", same, err)
	}
	bad := b
	bad.RepoID = other.ID
	if _, err = db.AddTicketBinding(t.Context(), bad); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("ownership conflict %v", err)
	}
	session, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 42, "bot", TaskRef("github", "owner", "repo", 7))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CorrectTicketBinding(t.Context(), b, b.Revision); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("used correction %v", err)
	}
	if _, err = db.DisableTicketBinding(t.Context(), b.ID, b.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(t.Context(), repo.ID, TicketConnection, tickets.Revision); err != nil {
		t.Fatal(err)
	}
	again, err := db.AdmitSession(t.Context(), repo, ref, 0, 0, 99, "replacement", "ignored")
	if err != nil || again.ID != session.ID {
		t.Fatalf("saved disabled admission %+v %v", again, err)
	}
	snapshot, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil || snapshot.ActorID != 42 || snapshot.TicketVersionID != tickets.ID {
		t.Fatalf("snapshot %+v %v", snapshot, err)
	}
	replacement := tickets
	replacement.Origin = "https://other.example/api/v3"
	replacement.Enabled = true
	replacement, err = db.SetConnection(t.Context(), replacement, tickets.Revision+1)
	if err != nil {
		t.Fatal(err)
	}
	secondRef := ref
	secondRef.Identity.Origin = replacement.Origin
	secondRef.Locator.Origin = replacement.Origin
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: replacement.ID, Ref: secondRef, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	second, err := db.AdmitSession(t.Context(), repo, secondRef, code.Revision, replacement.Revision, 42, "bot", TaskRef("github", "owner", "repo", 7))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == session.ID {
		t.Fatal("qualified collision reused saved work")
	}
	if second.TaskRef == session.TaskRef {
		t.Fatal("collision reused historical alias")
	}
	secondSnapshot, err := db.GetExecutionSnapshot(t.Context(), second.ID)
	if err != nil || secondSnapshot.NamingSuffix == "" {
		t.Fatalf("collision suffix not durable %+v %v", secondSnapshot, err)
	}
	if _, err = db.BindLegacySessionOrigin(t.Context(), second.ID, secondRef.Identity.Origin); err != nil {
		t.Fatalf("qualified saved origin %v", err)
	}
	if _, err = db.BindSessionTicket(t.Context(), second.ID, secondRef); err != nil {
		t.Fatalf("qualified observed mapping %v", err)
	}
	if _, err = db.FindSessionByIssue(t.Context(), 7, "owner", "repo"); err == nil {
		t.Fatal("bare number silently selected collision")
	}
	if got, err := db.GetSessionByTicket(t.Context(), secondRef.Identity); err != nil || got.ID != second.ID {
		t.Fatalf("qualified lookup %+v %v", got, err)
	}
}
func TestProjectAdmissionRollsBackOnStaleRevision(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "700")
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitSession(t.Context(), repo, ref, code.Revision+1, tickets.Revision, 42, "bot", "new-task"); !errors.Is(err, ErrConnectionConflict) {
		t.Fatalf("stale admission %v", err)
	}
	if _, err = db.GetSessionByTaskRef(t.Context(), "new-task"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial admission %v", err)
	}
}

func TestProjectBindingCorrectionCannotClaimExistingSessionIdentity(t *testing.T) {
	for _, sameProject := range []bool{false, true} {
		name := "another project"
		if sameProject {
			name = "same project"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			historicalRepo := testRepo(t, db, "owner", "history")
			destinationRepo := historicalRepo
			if !sameProject {
				destinationRepo = testRepo(t, db, "owner", "destination")
			}
			historicalSession, err := db.GetOrCreateSession(t.Context(), historicalRepo, 7)
			if err != nil {
				t.Fatal(err)
			}
			historicalRef := resolvedSessionTicket(historicalRepo.Slug(), "700")
			if _, err = db.BindSessionTicket(t.Context(), historicalSession.ID, historicalRef); err != nil {
				t.Fatal(err)
			}
			version, err := db.SetConnection(t.Context(), connectionFixture(destinationRepo, TicketConnection), 0)
			if err != nil {
				t.Fatal(err)
			}
			unusedRef := resolvedSessionTicket(destinationRepo.Slug(), "701")
			unusedRef.Locator.Key = "8"
			binding, err := db.AddTicketBinding(t.Context(), TicketBinding{RepoID: destinationRepo.ID, ConnectionVersionID: version.ID, Ref: unusedRef, Enabled: true, Provenance: "explicit"})
			if err != nil {
				t.Fatal(err)
			}
			correction := binding
			correction.Ref.Identity.RemoteID = historicalRef.Identity.RemoteID
			correction.Ref.Locator.Key = "7"
			if _, err = db.CorrectTicketBinding(t.Context(), correction, binding.Revision); !errors.Is(err, ErrBindingConflict) {
				t.Fatalf("historical identity claimed: %v", err)
			}
			retained, err := db.GetTicketBindingByID(t.Context(), binding.ID)
			if err != nil || retained != binding {
				t.Fatalf("failed correction changed unused binding %+v %v", retained, err)
			}
			saved, err := db.GetSessionByTicket(t.Context(), historicalRef.Identity)
			if err != nil || saved.ID != historicalSession.ID || saved.RepoID != historicalRepo.ID {
				t.Fatalf("historical ownership changed %+v %v", saved, err)
			}
		})
	}
}

func TestLegacyTicketIdentityCannotClaimExplicitBinding(t *testing.T) {
	db := newTestDB(t)
	owner := testRepo(t, db, "owner", "project")
	historical := testRepo(t, db, "owner", "history")
	version := connectionFixture(owner, TicketConnection)
	version.Destination = historical.Slug()
	version, err := db.SetConnection(t.Context(), version, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(historical.Slug(), "700")
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: owner.ID, ConnectionVersionID: version.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), historical, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.BindSessionTicket(t.Context(), session.ID, ref); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("claimed another project's binding: %v", err)
	}
	if _, err = db.GetSessionTicket(t.Context(), session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed claim left partial identity: %v", err)
	}
}

func TestAdmissionRejectsConflictingVerifiedContainerIdentity(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	code := connectionFixture(repo, CodeConnection)
	code.RemoteID = "original-code"
	code, err := db.SetConnection(t.Context(), code, 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "700")
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	taskRef := TaskRef("github", "owner", "repo", 7)
	if _, err = db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 42, "bot", taskRef, "replacement-code"); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("accepted changed code identity: %v", err)
	}
	if _, err = db.GetSessionByTaskRef(t.Context(), taskRef); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed admission left session: %v", err)
	}
}

func TestProjectBindingRejectsCompetingIdentityAtQualifiedAddress(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "enabled"
		if disabled {
			name = "tombstone"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			repo := testRepo(t, db, "owner", "repo")
			connection, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
			if err != nil {
				t.Fatal(err)
			}
			original, err := db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: connection.ID, Ref: resolvedSessionTicket(repo.Slug(), "700"), Enabled: true, Provenance: "explicit"})
			if err != nil {
				t.Fatal(err)
			}
			if disabled {
				if _, err := db.DisableTicketBinding(t.Context(), original.ID, original.Revision); err != nil {
					t.Fatal(err)
				}
			}
			competing := original
			competing.Ref.Identity.RemoteID = "701"
			if _, err := db.AddTicketBinding(t.Context(), competing); !errors.Is(err, ErrBindingConflict) {
				t.Fatalf("competing qualified address accepted: %v", err)
			}
			competing.Ref.Locator.Key = "8"
			candidate, err := db.AddTicketBinding(t.Context(), competing)
			if err != nil {
				t.Fatal(err)
			}
			candidate.Ref.Locator.Key = original.Ref.Locator.Key
			if _, err := db.CorrectTicketBinding(t.Context(), candidate, candidate.Revision); !errors.Is(err, ErrBindingConflict) {
				t.Fatalf("correction claimed retained address: %v", err)
			}
		})
	}
}

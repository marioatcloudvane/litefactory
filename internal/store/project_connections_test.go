package store

import (
	"encoding/json"
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

func TestBindingSourceImmutableEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, oldID, newID, recoveryID, verifiedID string
		allowed                                    bool
	}{
		{name: "missing"},
		{name: "matching", oldID: "101", newID: "101", allowed: true},
		{name: "replacement", oldID: "101", newID: "102"},
		{name: "verified", oldID: "101", verifiedID: "101", allowed: true},
		{name: "recovery", recoveryID: "101", verifiedID: "101", allowed: true},
		{name: "recovery conflict", oldID: "102", recoveryID: "101", verifiedID: "101"},
		{name: "verified conflict", oldID: "101", newID: "101", verifiedID: "102"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			repo := testRepo(t, db, "owner", "repo")
			code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
			if err != nil {
				t.Fatal(err)
			}
			v := connectionFixture(repo, TicketConnection)
			v.RemoteID = tc.oldID
			old, err := db.SetConnection(t.Context(), v, 0)
			if err != nil {
				t.Fatal(err)
			}
			if tc.recoveryID != "" {
				p, err := db.InitTicketRecoveryPosition(t.Context(), old, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				// Retain conflicting evidence as corruption would; admission must refuse it.
				p.ContainerID = tc.recoveryID
				raw, err := json.Marshal(p)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.sql.ExecContext(t.Context(), `UPDATE ticket_recovery_positions SET document=? WHERE connection_version_id=?`, string(raw), old.ID); err != nil {
					t.Fatal(err)
				}
			}
			ref := resolvedSessionTicket(repo.Slug(), "700")
			b, err := db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: old.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
			if err != nil {
				t.Fatal(err)
			}
			v.RemoteID = tc.newID
			next, err := db.SetConnection(t.Context(), v, old.Revision)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.AdmitSession(t.Context(), repo, ref, code.Revision, next.Revision, 42, "bot", "task", "", tc.verifiedID)
			if tc.allowed && err != nil {
				t.Fatal(err)
			}
			if !tc.allowed && !errors.Is(err, ErrBindingConflict) {
				t.Fatalf("want refusal, got %v", err)
			}
			if !tc.allowed {
				if _, err := db.GetSessionByTicket(t.Context(), ref.Identity); !errors.Is(err, ErrNotFound) {
					t.Fatalf("partial session: %v", err)
				}
			}
			saved, err := db.GetTicketBindingByID(t.Context(), b.ID)
			if err != nil || saved != b {
				t.Fatalf("binding changed: %+v %v", saved, err)
			}
		})
	}
}

// Retained bindings and execution versions can differ. Exercise them separately
// so current configuration or binding provenance cannot mask a saved conflict.
func TestManagedChildRetainedSource(t *testing.T) {
	for _, change := range []string{"same", "workspace", "team", "project", "origin", "provider", "destination", "container", "recovery", "unproven", "actor", "code", "code-actor", "locator", "binding-owner", "snapshot-only"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			repo := testRepo(t, db, "code", "repo")
			code, err := db.SetConnection(ctx, connectionFixture(repo, CodeConnection), 0)
			if err != nil {
				t.Fatal(err)
			}
			old, err := db.SetConnection(ctx, linearConnectionFixture(repo), 0)
			if err != nil {
				t.Fatal(err)
			}
			ref := func(id string) ticket.Ref {
				return ticket.Ref{Identity: ticket.Identity{Provider: old.Provider, Origin: old.Origin, RemoteID: id}, Locator: ticket.Locator{Provider: old.Provider, Origin: old.Origin, Container: old.Destination, Key: id}}
			}
			makeSession := func(number int, r ticket.Ref, bindingVersion, snapshotVersion ConnectionVersion, actor, codeID, codeActor int64, owner Repo) (Session, TicketBinding) {
				t.Helper()
				b, e := db.AddTicketBinding(ctx, TicketBinding{RepoID: owner.ID, ConnectionVersionID: bindingVersion.ID, Ref: r, Enabled: true, Provenance: "discovery"})
				if e != nil {
					t.Fatal(e)
				}
				s, e := db.GetOrCreateSession(ctx, owner, number)
				if e != nil {
					t.Fatal(e)
				}
				e = db.AttachExecutionSnapshot(ctx, ExecutionSnapshot{SessionID: s.ID, RepoID: owner.ID, BindingID: b.ID, CodeVersionID: codeID, TicketVersionID: snapshotVersion.ID, ActorID: actor, CodeActorID: codeActor})
				if e != nil {
					t.Fatal(e)
				}
				return s, b
			}
			parent, _ := makeSession(1, ref("parent"), old, old, 42, code.ID, 99, repo)
			next, err := db.DisableConnection(ctx, repo.ID, TicketConnection, old.Revision)
			if err != nil {
				t.Fatal(err)
			}
			next.Enabled = true
			next, err = db.SetConnection(ctx, next, next.Revision)
			if err != nil {
				t.Fatal(err)
			}
			childVersion := next
			actor, codeID, codeActor := int64(42), code.ID, int64(99)
			switch change {
			case "workspace", "snapshot-only":
				childVersion.Linear.WorkspaceID = "other"
			case "team":
				childVersion.Linear.TeamID = "other"
			case "project":
				childVersion.Linear.ProjectID = "other"
			case "origin":
				childVersion.Origin = "https://other.test"
			case "provider":
				childVersion.Provider = ticket.GitHub
				childVersion.Linear = LinearConfiguration{}
			case "destination":
				childVersion.Destination = "another/project"
			case "container":
				childVersion.RemoteID = "different"
			case "unproven":
				childVersion.Linear.WorkspaceID = ""
			case "actor":
				actor++
			case "code-actor":
				codeActor++
			case "code":
				v := code
				v.CredentialEnv = "OTHER"
				v, err = db.SetConnection(ctx, v, v.Revision)
				if err != nil {
					t.Fatal(err)
				}
				codeID = v.ID
			}
			// Direct SQL creates contradictory historical evidence that normal
			// configuration validation correctly prevents from being authored.
			if change != "same" && change != "actor" && change != "code" && change != "code-actor" && change != "locator" && change != "binding-owner" && change != "recovery" {
				config, _ := json.Marshal(childVersion.Linear)
				_, err = db.sql.ExecContext(ctx, `INSERT INTO project_connection_versions(repo_id,revision,role,provider,api_origin,destination,remote_id,credential_source,credential_env,enabled,provenance,created_at,linear_configuration) VALUES(?,100,'ticket',?,?,?,?,'env','TEST_TOKEN',1,'test',?,?)`, repo.ID, childVersion.Provider, childVersion.Origin, childVersion.Destination, childVersion.RemoteID, FormatTime(time.Now()), string(config))
				if err != nil {
					t.Fatal(err)
				}
				err = db.sql.QueryRowContext(ctx, `SELECT id FROM project_connection_versions WHERE repo_id=? AND role='ticket' AND revision=100`, repo.ID).Scan(&childVersion.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			// Admit a valid child first; snapshot-only uses a conflicting saved
			// version with a same-source binding and current connection.
			child, binding := makeSession(2, ref("child"), next, next, actor, codeID, codeActor, repo)
			if childVersion.ID != next.ID {
				// Retained snapshots are immutable in production. Rebuild the
				// fixture before the call to model an imported historical record.

				if _, err = db.sql.ExecContext(ctx, `UPDATE session_execution_snapshots SET ticket_version_id=? WHERE session_id=?`, childVersion.ID, child.ID); err != nil {
					t.Fatal(err)
				}
			}
			if change == "recovery" {
				_, err = db.InitTicketRecoveryPosition(ctx, next, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.sql.ExecContext(ctx, `UPDATE ticket_recovery_positions SET document=json_set(document,'$.ContainerID','other') WHERE connection_version_id=?`, next.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			requested := ref("child")
			if change == "locator" {
				requested.Locator.Key = "wrong"
			}
			if change == "binding-owner" {
				if _, err = db.sql.ExecContext(ctx, `UPDATE project_ticket_bindings SET repo_id=? WHERE id=?`, testRepo(t, db, "other", "repo").ID, binding.ID); err != nil {
					t.Fatal(err)
				}
				binding, err = db.GetTicketBindingByID(ctx, binding.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := db.GetExecutionSnapshot(ctx, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			got, err := db.AdmitManagedChild(ctx, parent.ID, requested, "ignored")
			if change == "same" {
				if err != nil || got.ID != child.ID {
					t.Fatalf("reuse: %+v %v", got, err)
				}
			} else if !errors.Is(err, ErrBindingConflict) {
				t.Fatalf("conflict accepted: %+v %v", got, err)
			}
			after, e := db.GetExecutionSnapshot(ctx, child.ID)
			if e != nil || before != after {
				t.Fatalf("snapshot mutated: %v", e)
			}
			saved, e := db.GetTicketBindingByID(ctx, binding.ID)
			if e != nil || saved != binding {
				t.Fatalf("binding mutated: %v", e)
			}
			if change == "same" {
				fresh, e := db.AdmitManagedChild(ctx, parent.ID, ref("fresh"), "fresh")
				if e != nil {
					t.Fatal(e)
				}
				snap, e := db.GetExecutionSnapshot(ctx, fresh.ID)
				if e != nil || snap.TicketVersionID != old.ID || snap.CodeVersionID != code.ID || snap.ActorID != 42 {
					t.Fatalf("fresh inheritance: %+v %v", snap, e)
				}
			}
		})
	}
}

// A story discovery admitted first under another same-source code/ticket pair
// is adopted by its managed parent; any other difference still conflicts.
func TestManagedChildAdoptsDiscoveredSameSourceChild(t *testing.T) {
	for _, change := range []string{"same", "actor", "code-remote", "empty-code-remote", "ticket-remote", "repo"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			repo := testRepo(t, db, "owner", "repo")
			ref := func(key, id string) ticket.Ref {
				return ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: id}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: "https://api.github.com", Container: repo.Slug(), Key: key}}
			}
			codeA, err := db.SetConnection(ctx, connectionFixture(repo, CodeConnection), 0)
			if err != nil {
				t.Fatal(err)
			}
			tv := connectionFixture(repo, TicketConnection)
			tv.RemoteID = "500"
			ticketA, err := db.SetConnection(ctx, tv, 0)
			if err != nil {
				t.Fatal(err)
			}
			childActor, childCode := int64(42), "900"
			switch change {
			case "actor":
				childActor++
			case "code-remote":
				childCode = "901"
			case "empty-code-remote":
				childCode = ""
			}
			childRef := ref("950", "5897118535")
			if _, err = db.AddTicketBinding(ctx, TicketBinding{RepoID: repo.ID, ConnectionVersionID: ticketA.ID, Ref: childRef, Enabled: true, Provenance: "discovery"}); err != nil {
				t.Fatal(err)
			}
			child, err := db.AdmitSession(ctx, repo, childRef, codeA.Revision, ticketA.Revision, childActor, "bot", "child", childCode, "500")
			if err != nil {
				t.Fatal(err)
			}
			codeNext := codeA
			codeNext.CredentialEnv = "OTHER"
			codeB, err := db.SetConnection(ctx, codeNext, codeA.Revision)
			if err != nil {
				t.Fatal(err)
			}
			ticketNext := ticketA
			ticketNext.CredentialEnv = "OTHER"
			ticketB, err := db.SetConnection(ctx, ticketNext, ticketA.Revision)
			if err != nil {
				t.Fatal(err)
			}
			parentRef := ref("649", "649649")
			if _, err = db.AddTicketBinding(ctx, TicketBinding{RepoID: repo.ID, ConnectionVersionID: ticketB.ID, Ref: parentRef, Enabled: true, Provenance: "discovery"}); err != nil {
				t.Fatal(err)
			}
			parent, err := db.AdmitSession(ctx, repo, parentRef, codeB.Revision, ticketB.Revision, 42, "bot", "parent", "900", "500")
			if err != nil {
				t.Fatal(err)
			}
			// Admission proves the ticket container, so model a contradictory
			// retained record directly, as an imported snapshot could carry.
			if change == "ticket-remote" {
				if _, err = db.sql.ExecContext(ctx, `UPDATE session_execution_snapshots SET ticket_remote_id='501' WHERE session_id=?`, child.ID); err != nil {
					t.Fatal(err)
				}
			}
			if change == "repo" {
				other := testRepo(t, db, "other", "repo")
				if _, err = db.sql.ExecContext(ctx, `UPDATE session_execution_snapshots SET repo_id=? WHERE session_id=?`, other.ID, child.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := db.GetExecutionSnapshot(ctx, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := db.GetTicketBinding(ctx, childRef.Identity)
			if err != nil {
				t.Fatal(err)
			}
			got, err := db.AdmitManagedChild(ctx, parent.ID, childRef, "ignored")
			if change == "same" {
				if err != nil || got.ID != child.ID {
					t.Fatalf("adopt: %+v %v", got, err)
				}
			} else if !errors.Is(err, ErrBindingConflict) {
				t.Fatalf("conflict accepted: %+v %v", got, err)
			}
			after, err := db.GetExecutionSnapshot(ctx, child.ID)
			if err != nil || before != after {
				t.Fatalf("snapshot mutated: %+v %v", after, err)
			}
			saved, err := db.GetTicketBinding(ctx, childRef.Identity)
			if err != nil || saved != binding {
				t.Fatalf("binding mutated: %+v %v", saved, err)
			}
		})
	}
}

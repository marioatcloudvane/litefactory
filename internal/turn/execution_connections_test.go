package turn

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestExecutionConnectionRetainsCodeAndBlocksCloneDrift(t *testing.T) {
	t.Parallel()
	cloneFixture := gittest.New(t)
	const originalRemote = "git@github.com:owner/code.git"
	cloneFixture.SetOriginURL(originalRemote)
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: cloneFixture.Dir, Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: originalRemote, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/code", CredentialSource: "env", CredentialEnv: "LF_TEST_PINNED", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/tasks", CredentialSource: "env", CredentialEnv: "LF_TEST_PINNED", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: tickets.Origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: tickets.Origin, Container: tickets.Destination, Key: "7"}}
	if _, err = db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 99, "factory-bot", "github:owner/tasks#7")
	if err != nil {
		t.Fatal(err)
	}
	changed := repo
	changed.Owner = "replacement"
	changed.Name = "code"
	changed.RemoteURL = "git@github.com:replacement/code.git"
	if _, err = db.UpsertRepo(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	code.Destination = "replacement/code"
	code.RemoteURL = changed.RemoteURL
	if _, err = db.SetConnection(t.Context(), code, code.Revision); err != nil {
		t.Fatal(err)
	}
	retained, err := executionRepository(t.Context(), db, session)
	if err != nil {
		t.Fatal(err)
	}
	if retained.Slug() != repo.Slug() || retained.RemoteURL != originalRemote || retained.Path != cloneFixture.Dir {
		t.Fatalf("saved code redirected to current registration: %+v", retained)
	}
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), cloneFixture.Dir)
	if err != nil {
		t.Fatal(err)
	}
	remoteChecks := 0
	options := Options{DB: db, VerifyExecutionConnection: func(_ context.Context, checked store.Session) error {
		remoteChecks++
		if checked.ID != session.ID {
			t.Errorf("verified session %d, want retained %d", checked.ID, session.ID)
		}
		return nil
	}}
	if err = verifyExecutionClone(t.Context(), options, session, clone); err != nil {
		t.Fatalf("original clone refused after configuration edit: %v", err)
	}
	if remoteChecks != 1 {
		t.Fatalf("remote identity verification count %d, want 1", remoteChecks)
	}
	cloneFixture.SetOriginURL(changed.RemoteURL)
	if err = verifyExecutionClone(t.Context(), options, session, clone); err == nil {
		t.Fatal("changed local remote allowed code effects")
	}
	if remoteChecks != 1 {
		t.Fatal("local clone drift reached the remote verification callback")
	}
	options.Git = binary
	options.CloneGate = newTestGate()
	session.Branch = "lf/7-retained"
	if _, err = IntegrationHead(t.Context(), options, session); err == nil || !strings.Contains(err.Error(), "original execution remote changed") {
		t.Fatalf("Validate fetched through a changed remote: %v", err)
	}
	if remoteChecks != 1 {
		t.Fatal("Validate clone drift reached the remote verification callback")
	}
	after, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil || after.CodeTarget.RemoteURL != originalRemote || after.CodeVersionID != code.ID {
		t.Fatalf("drift rewrote retained snapshot: %+v %v", after, err)
	}
}

package turn

import (
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Characterization of today's behaviour: when the live repos row and the pinned
// snapshot target differ, a pinned session reads the snapshot's base branch and
// an unpinned session reads the live repos row.
func TestExecutionRepositoryPinnedVersusLiveBaseBranch(t *testing.T) {
	t.Parallel()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "code"), Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: "git@github.com:owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "wt")})
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://api.github.com"
	code, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: "main", FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	unpinned, err := db.GetOrCreateSession(t.Context(), repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "101"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.Slug(), Key: "1"}}
	if _, err = db.BindSessionTicket(t.Context(), pinned.ID, ref); err != nil {
		t.Fatal(err)
	}
	binding, err := db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AttachExecutionSnapshot(t.Context(), store.ExecutionSnapshot{SessionID: pinned.ID, RepoID: repo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: 99, ActorLogin: "publisher"}); err != nil {
		t.Fatal(err)
	}
	// Move only the live row; the retained code version still says "main".
	live := repo
	live.BaseBranch = "develop"
	if _, err = db.UpsertRepo(t.Context(), live); err != nil {
		t.Fatal(err)
	}

	got, err := executionRepository(t.Context(), db, pinned)
	if err != nil || got.BaseBranch != "main" {
		t.Fatalf("pinned base=%q err=%v, want snapshot target main", got.BaseBranch, err)
	}
	got, err = executionRepository(t.Context(), db, unpinned)
	if err != nil || got.BaseBranch != "develop" {
		t.Fatalf("unpinned base=%q err=%v, want live row develop", got.BaseBranch, err)
	}
}

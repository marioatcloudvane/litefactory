package connectorstorage_test

import (
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/store"
)

// Backend identity must bind credentials and the extracted core target reader,
// including when independent narrow adapters are constructed for one handle.
func TestBindingKeepsCoreTargetsOnCredentialBackend(t *testing.T) {
	t.Parallel()
	open := func(path string) *store.DB {
		t.Helper()
		db, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		return db
	}
	path := filepath.Join(t.TempDir(), "first.db")
	first := open(path)
	foreign := open(filepath.Join(t.TempDir(), "foreign.db"))
	sameFile := open(path)
	for _, item := range []struct {
		db   *store.DB
		name string
	}{{first, "first"}, {foreign, "foreign"}} {
		_, err := item.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), item.name), Forge: store.ForgeGitHub, Owner: "acme", Name: item.name, RemoteURL: "git@github.com:acme/" + item.name + ".git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
	}
	bound := connectorstorage.New(first)
	if !bound.SameStore(connectorstorage.New(first)) {
		t.Fatal("separate adapters over one handle lost backend identity")
	}
	for _, other := range []*store.DB{foreign, sameFile} {
		if bound.SameStore(connectorstorage.New(other)) {
			t.Fatal("a different database handle was accepted as credential backend")
		}
	}
	if connectorstorage.New(nil) != nil {
		t.Fatal("nil backend produced usable ports")
	}
	repos, err := bound.ListConnectorRepositories(t.Context())
	if err != nil || len(repos) != 1 || repos[0].RepositoryRef != "github:acme/first" {
		t.Fatalf("core target enumeration escaped bound credential backend: %+v %v", repos, err)
	}
	foreignRepos, err := connectorstorage.New(foreign).ListConnectorRepositories(t.Context())
	if err != nil || len(foreignRepos) != 1 || foreignRepos[0].RepositoryRef != "github:acme/foreign" {
		t.Fatalf("independent backend target enumeration: %+v %v", foreignRepos, err)
	}
}

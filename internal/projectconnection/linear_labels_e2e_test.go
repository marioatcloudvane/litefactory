package projectconnection_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/localadmin"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// A Linear connection saved with no label mappings stays usable, reports that
// labels are not set up, refuses a mode change, and never writes to Linear.
func TestLinearConnectionWithoutLabelMapsDegradesWithoutWrites(t *testing.T) {
	home := t.TempDir()
	paths := config.NewPaths(home)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(home, "repo"), Forge: store.ForgeGitHub, Owner: "o", Name: "r", RemoteURL: "git@github.com:o/r.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(home, "wt")})
	if err != nil {
		t.Fatal(err)
	}
	var writes, lookups []string
	server := linearLabelServer(t, &writes, &lookups)

	v := linearLabelVersion(server.URL, store.LinearConfiguration{})
	v.ID, v.RepoID, v.Provenance = 0, repo.ID, "explicit"
	v.ClonePath, v.RemoteURL, v.BaseBranch, v.FeaturePrefix, v.WorktreeRoot = repo.Path, repo.RemoteURL, repo.BaseBranch, repo.FeaturePrefix, repo.WorktreeRoot
	saved, err := db.SetConnection(t.Context(), v, 0)
	if err != nil {
		t.Fatalf("connection without maps refused: %v", err)
	}

	f := projectconnection.NewFactory(projectconnection.Options{DB: db, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "test-token", true }}})
	o := f.Check(t.Context(), saved)
	if o.Status != "usable_for_checked_capabilities" || !strings.Contains(o.Remedy, "Labels are not set up.") || !strings.Contains(o.Remedy, "litefactory repo labels setup <project>") {
		t.Fatalf("check: %+v", o)
	}

	clients, err := f.Clients(t.Context(), saved, 0)
	if err != nil {
		t.Fatal(err)
	}
	origin := ticket.Origin(server.URL)
	ref := ticket.Ref{
		Identity: ticket.Identity{Provider: ticket.Linear, Origin: origin, RemoteID: "issue-id"},
		Locator:  ticket.Locator{Provider: ticket.Linear, Origin: origin, Container: "team/project", Key: "LIN-1"},
	}
	err = clients.Linear.SetMode(t.Context(), ref, "plan")
	var failure *ticket.Failure
	if !errors.As(err, &failure) || failure.Kind != ticket.Refused || !strings.Contains(err.Error(), "mode mapping is missing") {
		t.Fatalf("mode change: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	health, err := localadmin.InspectHealth(t.Context(), paths)
	if err != nil {
		t.Fatalf("daemon health refused empty map: %v", err)
	}
	if len(health.UnsupportedDoctor) != 1 {
		t.Fatalf("health: %+v", health)
	}
	if len(writes) != 0 {
		t.Fatalf("label search/create or issue write made: %v", writes)
	}
}

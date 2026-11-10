package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestRepositoryAccessRoleReadsInstalledTeamFile(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := config.NewPaths(home)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(home, "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "https://github.com/acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(home, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	read := repositoryAccessRole(paths, db)
	if text, err := read(t.Context(), repo); err == nil || text != "" || !strings.Contains(err.Error(), "link a team") {
		t.Fatalf("unlinked repository: %q %v", text, err)
	}

	dir := filepath.Join(paths.TeamsInstalledDir(), "local", "custom", "1.0.0")
	content := filepath.Join(dir, "content")
	write := func(name, body string) {
		t.Helper()
		file := filepath.Join(content, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("team.config", "research: research/research.dot\n")
	write("research/research.dot", "digraph research {}\n")
	receipt, err := json.Marshal(struct {
		Schema  int                `json:"schema"`
		Kind    string             `json:"kind"`
		Slug    string             `json:"slug"`
		Version string             `json:"version"`
		Origin  teampackage.Origin `json:"origin"`
	}{1, "local", "custom", "1.0.0", teampackage.Origin{Source: "https://example.com/teams.json", Slug: "standard", Version: "1.0.0", Checksum: strings.Repeat("a", 64), ClonedAt: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "receipt.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReplaceRepositoryTeamBinding(t.Context(), store.RepositoryTeamBinding{RepoID: repo.ID, Source: "local", Slug: "custom", Version: "1.0.0", Kind: "local"}); err != nil {
		t.Fatal(err)
	}
	if text, err := read(t.Context(), repo); err == nil || text != "" || !strings.Contains(err.Error(), teamsource.AccessClassifierPath) {
		t.Fatalf("team without access role: %q %v", text, err)
	}
	write(teamsource.AccessClassifierPath, "fixture team access role\n")
	if text, err := read(t.Context(), repo); err != nil || text != "fixture team access role\n" {
		t.Fatalf("team access role: %q %v", text, err)
	}
}

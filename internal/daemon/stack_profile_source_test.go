package daemon

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositorySourceRetainsModernProfileAndRefusesProfilelessUnknownTeam(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := config.NewPaths(home)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(home, "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "https://github.com/acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(home, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	specs, err := turn.ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	captured, err := teamsourcetest.CaptureImplement(t.Context(), specs)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(paths.TeamsInstalledDir(), "local", "custom", "1.0.0")
	content := filepath.Join(dir, "content")
	put := func(name string, raw []byte) {
		t.Helper()
		file := filepath.Join(content, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, raw := range captured.Files {
		put("implement/"+name, raw)
	}
	put("team.config", []byte("implement: implement/implement.dot\n"))
	profile := []byte(`{"version":1,"guidance":[{"id":"synthetic-stack","skill":"implement/skills/stack/SKILL.md"}]}`)
	put(config.StackProfilePath, profile)
	put("implement/skills/stack/SKILL.md", []byte("---\nname: stack\ndescription: Synthetic stack advice\n---\nAdvice."))
	receipt, err := json.Marshal(map[string]any{"schema": 1, "kind": "local", "slug": "custom", "version": "1.0.0", "origin": map[string]any{"source": "https://example.com/teams.json", "slug": "unknown", "version": "1.0.0", "checksum": strings.Repeat("a", 64), "cloned_at": "2026-10-10T00:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "receipt.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReplaceRepositoryTeamBinding(t.Context(), store.RepositoryTeamBinding{RepoID: repo.ID, Source: "local", Slug: "custom", Version: "1.0.0", Kind: "local"}); err != nil {
		t.Fatal(err)
	}
	resolver, err := repositoryWorkflowSource(paths, db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver(t.Context(), repo, agent.WorkflowImplement, specs)
	if err != nil {
		t.Fatal(err)
	}
	if got.StackProfile == nil || string(got.StackProfile.BaseBytes) != string(profile) || got.Origin.Kind != "local" || got.Origin.Slug != "custom" {
		t.Fatal("profile or local provenance missing")
	}
	if string(got.StackPackageFiles["implement/skills/stack/SKILL.md"]) == "" {
		t.Fatal("selected guidance closure missing from capture")
	}
	if err = os.Remove(filepath.Join(content, config.StackProfilePath)); err != nil {
		t.Fatal(err)
	}
	if _, err = resolver(t.Context(), repo, agent.WorkflowImplement, specs); err == nil {
		t.Fatal("unknown profileless local team fell back to Go")
	}
	// Editable clones of explicit historical Go releases retain profileless support.
	var decoded map[string]any
	if err = json.Unmarshal(receipt, &decoded); err != nil {
		t.Fatal(err)
	}
	origin, ok := decoded["origin"].(map[string]any)
	if !ok {
		t.Fatal("fixture receipt origin is not an object")
	}
	origin["slug"] = "go-development"
	origin["version"] = "1.1.1"
	receipt, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "receipt.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := resolver(t.Context(), repo, agent.WorkflowImplement, specs)
	if err != nil {
		t.Fatal("historical Go local clone refused", err)
	}
	if legacy.StackProfile != nil || legacy.Origin.Kind != "local" {
		t.Fatal("historical clone semantics changed")
	}
}

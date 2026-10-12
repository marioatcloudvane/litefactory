package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestRepositoryWorkflowSourceEditableSelectionAcrossSixWorkflows(t *testing.T) {
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
	variants := []struct {
		workflow agent.Workflow
		specs    func() (graphexec.Registry, error)
		capture  func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error)
	}{
		{agent.WorkflowResearch, turn.ResearchSpecs, teamsource.CaptureResearch},
		{agent.WorkflowPlan, turn.PlanSpecs, teamsource.CapturePlan},
		{agent.WorkflowImplement, turn.ImplementSpecs, teamsource.CaptureImplement},
		{agent.WorkflowValidate, turn.ValidateFlowSpecs, teamsource.CaptureValidate},
		{agent.WorkflowSpike, turn.SpikeSpecs, teamsource.CaptureSpike},
		{agent.WorkflowBug, turn.SimplifiedBugSpecs, teamsource.CaptureBugParent},
	}
	dir := filepath.Join(paths.TeamsInstalledDir(), "local", "custom", "1.0.0")
	content := filepath.Join(dir, "content")
	write := func(name string, raw []byte) {
		t.Helper()
		file := filepath.Join(content, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var mapping strings.Builder
	for _, v := range variants {
		specs, err := v.specs()
		if err != nil {
			t.Fatal(err)
		}
		def, err := v.capture(t.Context(), specs)
		if err != nil {
			t.Fatal(err)
		}
		for name, raw := range def.Files {
			write(string(v.workflow)+"/"+name, raw)
		}
		mapping.WriteString(string(v.workflow) + ": " + string(v.workflow) + "/" + string(v.workflow) + ".dot\n")
	}
	write("team.config", []byte(mapping.String()))
	write(config.StackProfilePath, []byte(`{"version":1,"guidance":[{"id":"fixture-stack","skill":"research/skills/fixture-stack/SKILL.md"}]}`))
	write("research/skills/fixture-stack/SKILL.md", []byte("---\nname: fixture-stack\ndescription: Synthetic retained team fixture guidance\n---\nAdvice.\n"))
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
	receiptPath := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(receiptPath, receipt, 0600); err != nil {
		t.Fatal(err)
	}
	resolver, err := repositoryWorkflowSource(paths, db)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := turn.ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	unlinked, err := resolver(t.Context(), repo, agent.WorkflowImplement, specs)
	if err == nil || unlinked != nil || !strings.Contains(err.Error(), "no linked installed team") {
		t.Fatalf("unlinked source: %v %v", unlinked, err)
	}
	var refusal *workflowRefusal
	if !errors.As(err, &refusal) || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing typed cause: %v", err)
	}
	if _, err := db.ReplaceRepositoryTeamBinding(t.Context(), store.RepositoryTeamBinding{RepoID: repo.ID, Source: "local", Slug: "custom", Version: "1.0.0", Kind: "local"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range variants {
		t.Run(string(v.workflow), func(t *testing.T) {
			specs, err := v.specs()
			if err != nil {
				t.Fatal(err)
			}
			def, err := resolver(t.Context(), repo, v.workflow, specs)
			if err != nil {
				t.Fatal(err)
			}
			if def.Profile.Team != "go-development" || def.Origin.Kind != "local" || def.Origin.Slug != "custom" || len(def.Origin.TeamConfig) == 0 {
				t.Fatalf("source lost provenance: %+v", def.Origin)
			}
			graph := string(v.workflow) + "/" + string(v.workflow) + ".dot"
			raw, err := os.ReadFile(filepath.Join(content, graph))
			if err != nil {
				t.Fatal(err)
			}
			write(graph, append(raw, []byte("\n// accepted local edit\n")...))
			edited, err := resolver(t.Context(), repo, v.workflow, specs)
			if err != nil || edited.Digest == def.Digest {
				t.Fatalf("edit not freshly captured: %v", err)
			}
		})
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || string(after) != string(receipt) {
		t.Fatal("admission changed permanent clone origin")
	}
	// Retain the same offline content as an immutable fetched release.
	source := "https://example.com/teams.json"
	sourceHash := sha256.Sum256([]byte(source))
	fetchedDir := filepath.Join(paths.TeamsInstalledDir(), hex.EncodeToString(sourceHash[:]), "custom", "1.0.0")
	var inventory []teampackage.File
	err = filepath.WalkDir(content, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, e := filepath.Rel(content, file)
		if e != nil {
			return e
		}
		raw, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		inventory = append(inventory, teampackage.File{Path: filepath.ToSlash(relative), SHA256: hex.EncodeToString(sum[:])})
		target := filepath.Join(fetchedDir, "content", relative)
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return e
		}
		return os.WriteFile(target, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(inventory, func(a, b teampackage.File) int { return strings.Compare(a.Path, b.Path) })
	var inventoryText strings.Builder
	for _, file := range inventory {
		inventoryText.WriteString(file.Path + "\x00" + file.SHA256 + "\n")
	}
	releaseHash := sha256.Sum256([]byte(inventoryText.String()))
	checksum := hex.EncodeToString(releaseHash[:])
	release := teampackage.Release{Name: "offline fetched fixture", Tags: []string{}, Slug: "custom", Version: "1.0.0", Download: "release.tar.gz", Files: inventory, Checksum: checksum}
	fetchedReceipt, err := json.Marshal(struct {
		Schema   int                 `json:"schema"`
		Source   string              `json:"source"`
		Release  teampackage.Release `json:"release"`
		Checksum string              `json:"checksum"`
	}{1, source, release, checksum})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(fetchedDir, "receipt.json"), fetchedReceipt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReplaceRepositoryTeamBinding(t.Context(), store.RepositoryTeamBinding{RepoID: repo.ID, Source: source, Slug: "custom", Version: "1.0.0", Kind: "fetched", ReleaseDigest: checksum}); err != nil {
		t.Fatal(err)
	}
	for _, v := range variants {
		t.Run("fetched/"+string(v.workflow), func(t *testing.T) {
			registry, e := v.specs()
			if e != nil {
				t.Fatal(e)
			}
			def, e := resolver(t.Context(), repo, v.workflow, registry)
			if e != nil {
				t.Fatal(e)
			}
			if def.Origin.Kind != "fetched" || def.Origin.ReleaseDigest != checksum || def.Origin.Source != source {
				t.Fatalf("lost fetched identity: %+v", def.Origin)
			}
		})
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = resolver(cancelled, repo, agent.WorkflowImplement, specs); !errors.Is(err, context.Canceled) || errors.As(err, &refusal) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if err = os.WriteFile(filepath.Join(fetchedDir, "content", "implement", "implement.dot"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = resolver(t.Context(), repo, agent.WorkflowImplement, specs); err == nil || errors.As(err, &refusal) || !strings.Contains(err.Error(), repo.Path) || !strings.Contains(err.Error(), "implement") {
		t.Fatalf("corrupt release accepted: %v", err)
	}
	if _, err = db.ReplaceRepositoryTeamBinding(t.Context(), store.RepositoryTeamBinding{RepoID: repo.ID, Source: "local", Slug: "custom", Version: "1.0.0", Kind: "local"}); err != nil {
		t.Fatal(err)
	}
	write("team.config", []byte("research: research/research.dot\n"))
	if _, err := resolver(t.Context(), repo, agent.WorkflowImplement, specs); err == nil || !errors.As(err, &refusal) || !strings.Contains(err.Error(), repo.Path) || !strings.Contains(err.Error(), "available workflows: research") {
		t.Fatalf("missing workflow diagnostic: %v", err)
	}
}

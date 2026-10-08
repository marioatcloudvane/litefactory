package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	if err != nil || unlinked != nil {
		t.Fatalf("unlinked source: %v %v", unlinked, err)
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
			if def.Profile.Team != "builtin" || def.Origin.Kind != "local" || def.Origin.Slug != "custom" || len(def.Origin.TeamConfig) == 0 {
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
	write("team.config", []byte("research: research/research.dot\n"))
	if _, err := resolver(t.Context(), repo, agent.WorkflowImplement, specs); err == nil || !strings.Contains(err.Error(), repo.Path) || !strings.Contains(err.Error(), "available workflows: research") {
		t.Fatalf("missing workflow diagnostic: %v", err)
	}
}

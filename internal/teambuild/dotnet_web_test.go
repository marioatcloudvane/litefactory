package teambuild

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

const dotnetTeam = "dotnet-web"

func dotnetProfile(t *testing.T) config.ResolvedStackProfile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(resolvedDir(t, dotnetTeam), config.StackProfilePath))
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDotnetWebExactInstallAndMissingGuidance(t *testing.T) {
	version := sourceVersion(t, dotnetTeam)
	_, installed := install(t, repoSource(), dotnetTeam)
	if installed.Identity.Slug != dotnetTeam || installed.Identity.Version != version || len(installed.Workflows) != 6 {
		t.Fatal("exact identity or six workflows missing")
	}
	for _, path := range []string{"team.config", "stack-profile.json", "plan/skills/blazor-guidance/compatibility.md", "plan/skills/blazor-guidance/practices.md"} {
		found := false
		for _, f := range installed.Release.Files {
			found = found || f.Path == path
		}
		if !found {
			t.Fatal("missing verified inventory resource", path)
		}
	}
	dir := copySource(t)
	if err := os.Remove(filepath.Join(dir, dotnetTeam, "plan/skills/blazor-guidance/practices.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Build(t.Context(), dir, dotnetTeam); err == nil {
		t.Fatal("missing guidance admitted")
	}
}

func TestDotnetWebCapturedGuidanceAndProviderReplay(t *testing.T) {
	p := dotnetProfile(t)
	source := os.DirFS(resolvedDir(t, dotnetTeam))
	guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, p.Effective)
	if err != nil {
		t.Fatal(err)
	}
	builders := map[agent.Workflow]func() (graphexec.Registry, error){agent.WorkflowResearch: turn.ResearchSpecs, agent.WorkflowPlan: turn.PlanSpecs, agent.WorkflowImplement: turn.ImplementSpecs, agent.WorkflowValidate: turn.ValidateFlowSpecs, agent.WorkflowSpike: turn.SpikeSpecs, agent.WorkflowBug: turn.BugSpecs}
	for mode, builder := range builders {
		t.Run(string(mode), func(t *testing.T) {
			registry, err := builder()
			if err != nil {
				t.Fatal(err)
			}
			sub, err := fs.Sub(source, string(mode))
			if err != nil {
				t.Fatal(err)
			}
			d, err := teamsource.CaptureWorkflowIdentity(t.Context(), sub, string(mode)+".dot", dotnetTeam, mode, registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := teamsource.AttachStackProfile(t.Context(), d, p, strings.Repeat("a", 40), guidance); err != nil {
				t.Fatal(err)
			}
			replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(d.Files), string(mode)+".dot", dotnetTeam, mode, registry)
			if err != nil {
				t.Fatal(err)
			}
			if replay.Digest != d.Digest || replay.StackProfile.EffectiveDigest != p.EffectiveDigest {
				t.Fatal("resume/fallback did not retain exact profile")
			}
			for id, node := range replay.Nodes {
				for _, provider := range []string{"claude", "codex"} {
					files, err := teamsource.NativeWorkflow(provider, node, "model", []string{"Read"}, "read-only")
					if err != nil {
						t.Fatal(id, provider, err)
					}
					found := false
					for _, f := range files {
						if f.Path == "plugin/skills/blazor-guidance/practices.md" {
							found = true
							if !bytes.Equal(f.Bytes, guidance["plan/skills/blazor-guidance/practices.md"]) {
								t.Fatal("provider changed captured guidance")
							}
						}
					}
					if !found {
						t.Fatal(id, provider, "guidance missing")
					}
				}
			}
		})
	}
	changed := slices.Clone(p.Effective.Checks)
	changed[0].CWD = "src"
	override, err := json.Marshal(config.StackOverride{Version: 1, Checks: &changed})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := config.ResolveStackProfile(p.BaseBytes, override)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.EffectiveDigest == p.EffectiveDigest {
		t.Fatal("fresh admission ignored permitted recipe override")
	}
	// Keep the original profile's captured bytes independent of the new override.
	if err := p.ValidateRetained(); err != nil {
		t.Fatal("fresh override changed original capture", err)
	}
	if original := dotnetProfile(t); original.Effective.Checks[0].CWD != "." {
		t.Fatal("override modified source bytes")
	}
}

func TestDotnetWebCompatibilityAndGuidance(t *testing.T) {
	for _, tt := range []struct{ name, dir string }{
		{"resolved source", resolvedDir(t, dotnetTeam)},
		{"built team", builtDir(t, dotnetTeam)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(tt.dir, config.StackProfilePath))
			if err != nil {
				t.Fatal(err)
			}
			p, err := config.ResolveStackProfile(raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			matrix, err := os.ReadFile(filepath.Join(tt.dir, "plan/skills/blazor-guidance/compatibility.md"))
			if err != nil {
				t.Fatal(err)
			}
			practices, err := os.ReadFile(filepath.Join(tt.dir, "plan/skills/blazor-guidance/practices.md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, pin := range []string{"10.0.401", "10.0.12", "net10.0"} {
				if !bytes.Contains(matrix, []byte(pin)) || !strings.Contains(p.Effective.Checks[0].Argv[2], pin) {
					t.Fatal("profile/matrix pin mismatch", pin)
				}
			}
			if len(practices) == 0 {
				t.Fatal("practices guidance is empty")
			}
			for _, c := range p.Effective.Checks {
				if c.TimeoutSeconds <= 0 || c.Argv[0] != "sh" {
					t.Fatal("unbounded or unexpected recipe")
				}
			}
		})
	}
}

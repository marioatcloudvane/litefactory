package teamrelease

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

const dotnetSource = "teams/source/dotnet-web"

// dotnetRelease is the published directory of the current dotnet-web release.
func dotnetRelease(t *testing.T) string {
	t.Helper()
	return "../../teams/dotnet-web/v" + sourceVersion(t, "../../"+dotnetSource)
}

func dotnetProfile(t *testing.T) config.ResolvedStackProfile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dotnetRelease(t), config.StackProfilePath))
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDotnetWebReleaseGenerationAndExactInstall(t *testing.T) {
	root := canonicalTemp(t)
	for _, dir := range []string{"teams"} {
		for name, raw := range snapshot(t, "../../"+dir) {
			put(t, filepath.Join(root, dir, name), raw)
		}
	}
	// Canonical source may advance independently of immutable published releases.
	// Choose a fixture-only version, never changing repository release metadata.
	metadataPath := filepath.Join(root, dotnetSource, "release.json")
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var candidate map[string]any
	if err := json.Unmarshal(metadata, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate["version"] = "9999.0.0"
	candidate["download"] = "dotnet-web/v9999.0.0"
	metadata, err = json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	put(t, metadataPath, metadata)
	if err := GenerateSources(t.Context(), root, false, []string{dotnetSource}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, filepath.Join(root, "teams"))
	if err := GenerateSources(t.Context(), root, true, []string{dotnetSource}); err != nil {
		t.Fatal(err)
	}
	if err := GenerateSources(t.Context(), root, false, []string{dotnetSource}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Join(root, "teams"))) {
		t.Fatal("regeneration changed catalogue or historical release bytes")
	}
	// Rebuild this team's current release from source, retaining every other release
	// (including this team's history).
	version := sourceVersion(t, filepath.Join(root, dotnetSource))
	raw, err := os.ReadFile(filepath.Join(root, "teams/teams.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue teampackage.Catalogue
	if err := json.Unmarshal(raw, &catalogue); err != nil {
		t.Fatal(err)
	}
	releases := catalogue.Releases[:0]
	for _, release := range catalogue.Releases {
		if release.Slug != "dotnet-web" || release.Version != version {
			releases = append(releases, release)
		}
	}
	catalogue.Releases = releases
	raw, err = json.Marshal(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "teams/teams.json"), raw)
	if err := os.RemoveAll(filepath.Join(root, "teams/dotnet-web", "v"+version)); err != nil {
		t.Fatal(err)
	}
	if err := GenerateSources(t.Context(), root, false, []string{dotnetSource}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Join(root, "teams"))) {
		t.Fatal("fresh generation changed released bytes")
	}

	m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	installed, _, err := m.Install(t.Context(), filepath.Join(root, "teams/teams.json"), "dotnet-web", version)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Identity.Slug != "dotnet-web" || installed.Identity.Version != version || len(installed.Workflows) != 6 {
		t.Fatal("exact identity or six workflows missing")
	}
	for _, path := range []string{"team.config", "stack-profile.json", "plan/skills/blazor-guidance/compatibility.md", "plan/skills/blazor-guidance/practices.md"} {
		found := false
		for _, f := range installed.Release.Files {
			if f.Path == path {
				found = true
			}
		}
		if !found {
			t.Fatal("missing verified inventory resource", path)
		}
	}
	// Invalid source closure must leave all published bytes intact.
	path := filepath.Join(root, dotnetSource, "plan/skills/blazor-guidance/practices.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	published := snapshot(t, filepath.Join(root, "teams"))
	if err := GenerateSources(t.Context(), root, false, []string{dotnetSource}); err == nil {
		t.Fatal("missing guidance admitted")
	}
	if !reflect.DeepEqual(published, snapshot(t, filepath.Join(root, "teams"))) {
		t.Fatal("invalid source changed publication")
	}
}

func TestDotnetWebCapturedGuidanceAndProviderReplay(t *testing.T) {
	p := dotnetProfile(t)
	source := os.DirFS(dotnetRelease(t))
	guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, p.Effective)
	if err != nil {
		t.Fatal(err)
	}
	builders := map[agent.Workflow]func() (graphexec.Registry, error){agent.WorkflowResearch: turn.ResearchSpecs, agent.WorkflowPlan: turn.PlanSpecs, agent.WorkflowImplement: turn.ImplementSpecs, agent.WorkflowValidate: turn.ValidateFlowSpecs, agent.WorkflowSpike: turn.SpikeSpecs, agent.WorkflowBug: turn.SimplifiedBugSpecs}
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
			d, err := teamsource.CaptureWorkflowIdentity(t.Context(), sub, string(mode)+".dot", "dotnet-web", mode, registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := teamsource.AttachStackProfile(t.Context(), d, p, strings.Repeat("a", 40), guidance); err != nil {
				t.Fatal(err)
			}
			replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(d.Files), string(mode)+".dot", "dotnet-web", mode, registry)
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
	original := dotnetProfile(t)
	if original.Effective.Checks[0].CWD != "." {
		t.Fatal("override modified released bytes")
	}
}

func TestDotnetWebCompatibilityAndGuidance(t *testing.T) {
	p := dotnetProfile(t)
	matrix, err := os.ReadFile(filepath.Join(dotnetRelease(t), "plan/skills/blazor-guidance/compatibility.md"))
	if err != nil {
		t.Fatal(err)
	}
	practices, err := os.ReadFile(filepath.Join(dotnetRelease(t), "plan/skills/blazor-guidance/practices.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{"10.0.401", "10.0.12", "net10.0"} {
		if !bytes.Contains(matrix, []byte(pin)) || !strings.Contains(p.Effective.Checks[0].Argv[2], pin) {
			t.Fatal("profile/matrix pin mismatch", pin)
		}
	}
	for _, term := range []string{"net10.0", "SDK default", "macOS or Linux", "wasm-tools", "AOT", "NuGet", "Node.js"} {
		if !bytes.Contains(matrix, []byte(term)) {
			t.Fatal("compatibility omission", term)
		}
	}
	for _, term := range []string{"Static SSR", "Interactive Server", "Interactive WebAssembly", "Auto", "Standalone WebAssembly", "Prerendering", "DI lifetimes", "EditForm", "keyboard", "state ownership", "JavaScript", "authorization", "circuit", "Validate"} {
		if !bytes.Contains(practices, []byte(term)) {
			t.Fatal("guidance omission", term)
		}
	}
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike", "bug"} {
		shared, err := os.ReadFile(filepath.Join("../teamsource", mode, mode+".dot"))
		if err != nil {
			t.Fatal(err)
		}
		released, err := os.ReadFile(filepath.Join(dotnetRelease(t), mode, mode+".dot"))
		if err != nil {
			t.Fatal(err)
		}
		shared = legacySourceWithoutPreparationDeferral(t, mode, shared)
		shared = withoutStandardsGrant(t, shared)
		if mode == "bug" {
			shared = legacyBugSourceWithoutSyncGate(t, shared)
		}
		if mode == "implement" {
			shared = legacyImplementSourceWithoutOwnerResumeSync(t, withoutGoStoryWorkerSlotPolicy(t, shared))
		}
		if mode == "spike" {
			shared = legacySpikeSourceWithoutRepair(t, shared)
		}
		want := bytes.ReplaceAll(shared, []byte("go-development"), []byte("dotnet-web"))
		want = bytes.ReplaceAll(want, []byte(",litefactory-rules"), []byte(",dotnet-web-engineering"))
		if !bytes.Equal(want, released) && (mode != "implement" || !pendingWorkerSyncMatchesRelease(want, released)) {
			t.Fatal("team selection changed shared graph gates", mode)
		}
	}

	for _, c := range p.Effective.Checks {
		if c.TimeoutSeconds <= 0 || c.Argv[0] != "sh" {
			t.Fatal("unbounded or unexpected recipe")
		}
	}
}

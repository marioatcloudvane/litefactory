package teamsource_test

import (
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/skillclosure"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestOwnerReportSourceCaptureNativeDeliveryAndColdReplay checks availability,
// not the wording of reports or whether an agent will follow the guidance.
func TestOwnerReportSourceCaptureNativeDeliveryAndColdReplay(t *testing.T) {
	workflows := []struct {
		workflow agent.Workflow
		registry func() (graphexec.Registry, error)
		nodes    []string
	}{
		{agent.WorkflowResearch, turn.ResearchSpecs, []string{"investigate", "shape"}},
		{agent.WorkflowPlan, turn.PlanSpecs, []string{"set_agent", "story_agent", "technical_agent", "acceptance_agent"}},
		{agent.WorkflowImplement, turn.ImplementSpecs, []string{"worker_agent", "sync_start_resolve", "sync_done_resolve"}},
		{agent.WorkflowValidate, turn.ValidateFlowSpecs, []string{"repair_agent", "review:validate-acceptance", "review:validate-code", "review:validate-architecture"}},
		{agent.WorkflowBug, turn.SimplifiedBugSpecs, []string{"analyst", "fixer"}},
		{agent.WorkflowSpike, turn.SpikeSpecs, []string{"probe_agent", "fix_agent", "refix_agent"}},
	}
	for _, team := range []string{"go-development", "swift-ios", "python-fastapi", "dotnet-web"} {
		t.Run(team, func(t *testing.T) {
			root, err := os.OpenRoot(teamsourcetest.RepoRoot())
			if err != nil {
				t.Fatal(err)
			}
			// Every team inherits base-team, which holds no stack profile of its own.
			sourcePath, admittedTeam := "teams/source/"+team, team
			source, err := teamsource.ResolveSource(t.Context(), root, sourcePath, admittedTeam)
			if closeErr := root.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := config.ResolveStackProfile(source["stack-profile.json"], nil)
			if err != nil {
				t.Fatal(err)
			}
			selected := resolved.Effective
			selected.Guidance = nil
			for _, entry := range resolved.Effective.Guidance {
				if entry.ID == "owner-report" {
					selected.Guidance = append(selected.Guidance, entry)
				}
			}
			if len(selected.Guidance) != 1 || selected.Guidance[0].Skill != "plan/skills/owner-report/SKILL.md" {
				t.Fatal("profile must select the single canonical owner-report skill")
			}
			packageFS := ownerReportMapFS(source)
			ownerClosure, err := teamsource.CaptureStackGuidance(t.Context(), packageFS, selected)
			if err != nil {
				t.Fatal(err)
			}
			header, _, err := skillclosure.Parse(ownerClosure[selected.Guidance[0].Skill], "owner-report")
			if err != nil {
				t.Fatal(err)
			}
			for _, resource := range []string{"mode-guidance.md", "examples.md"} {
				if !slices.Contains(header.Resources, resource) {
					t.Fatalf("owner-report does not declare %s", resource)
				}
			}
			guidance, err := teamsource.CaptureStackGuidance(t.Context(), packageFS, resolved.Effective)
			if err != nil {
				t.Fatal(err)
			}
			type capture struct {
				definition *teamsource.WorkflowDefinition
				registry   graphexec.Registry
			}
			captures := make([]capture, len(workflows))
			for i, variant := range workflows {
				registry, err := variant.registry()
				if err != nil {
					t.Fatal(err)
				}
				workflowFS, err := fs.Sub(packageFS, string(variant.workflow))
				if err != nil {
					t.Fatal(err)
				}
				definition, err := teamsource.CaptureWorkflowIdentity(t.Context(), workflowFS, string(variant.workflow)+".dot", admittedTeam, variant.workflow, registry)
				if err != nil {
					t.Fatalf("%s capture: %v", variant.workflow, err)
				}
				if err := teamsource.AttachStackProfile(t.Context(), definition, resolved, strings.Repeat("a", 40), guidance); err != nil {
					t.Fatalf("%s attach: %v", variant.workflow, err)
				}
				captures[i] = capture{definition, registry}
			}
			// Replays below have only owned retained files. Remove every mutable
			// source entry and selected guidance input before restoring a workflow.
			clear(source)
			clear(packageFS)
			clear(guidance)
			for i, variant := range workflows {
				t.Run(string(variant.workflow), func(t *testing.T) {
					fresh := captures[i].definition
					replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(fresh.Files), string(variant.workflow)+".dot", admittedTeam, variant.workflow, captures[i].registry)
					if err != nil {
						t.Fatal(err)
					}
					for _, definition := range []*teamsource.WorkflowDefinition{fresh, replay} {
						if definition.StackProfile == nil {
							t.Fatal("selected stack admission was not retained")
						}
						for _, id := range variant.nodes {
							node, ok := definition.Nodes[id]
							if !ok {
								t.Fatalf("missing reporting node %s", id)
							}
							if !slices.Contains(node.Skills, "owner-report") {
								t.Fatalf("%s does not select owner-report", id)
							}
							for _, provider := range []string{"claude", "codex"} {
								delivered, err := teamsource.NativeWorkflow(provider, node, "model", []string{"Read"}, "read-only")
								if err != nil {
									t.Fatalf("%s/%s: %v", id, provider, err)
								}
								paths := map[string]bool{}
								for _, file := range delivered {
									paths[file.Path] = true
								}
								for name := range ownerClosure {
									_, relative, ok := strings.Cut(name, "/skills/")
									if !ok {
										t.Fatalf("invalid closure path %s", name)
									}
									skill, resource, _ := strings.Cut(relative, "/")
									if skill == "shared" {
										skill, resource, _ = strings.Cut(resource, "/")
									}
									native := path.Join("plugin/skills", skill, resource)
									if !paths[native] {
										t.Fatalf("%s/%s missing declared closure resource %s", id, provider, native)
									}
								}
							}
						}
					}
				})
			}
		})
	}
}

func ownerReportMapFS(files map[string][]byte) fstest.MapFS {
	result := fstest.MapFS{}
	for name, raw := range files {
		result[name] = &fstest.MapFile{Data: slices.Clone(raw)}
	}
	return result
}

package turn

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestConsolidatedWorkflowVerifiedLaunchDelivery(t *testing.T) {
	captures := map[agent.Workflow]func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error){agent.WorkflowResearch: teamsource.CaptureResearch, agent.WorkflowPlan: teamsource.CapturePlan, agent.WorkflowImplement: teamsource.CaptureImplement, agent.WorkflowValidate: teamsource.CaptureValidate, agent.WorkflowSpike: teamsource.CaptureSpike, agent.WorkflowBug: teamsource.CaptureBugParent}
	builders := map[agent.Workflow]func() (graphexec.Registry, error){agent.WorkflowResearch: ResearchSpecs, agent.WorkflowPlan: PlanSpecs, agent.WorkflowImplement: ImplementSpecs, agent.WorkflowValidate: ValidateFlowSpecs, agent.WorkflowSpike: SpikeSpecs, agent.WorkflowBug: SimplifiedBugSpecs}
	for _, policy := range RegisteredWorkflowPolicies() {
		w, err := agent.ResolveWorkflow(policy.Workflow, policy.Mode)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(string(w), func(t *testing.T) {
			specs, err := builders[w]()
			if err != nil {
				t.Fatal(err)
			}
			d, err := captures[w](t.Context(), specs)
			if err != nil {
				t.Fatal(err)
			}
			if d.Profile.Team != "go-development" {
				t.Fatal("wrong current-source identity")
			}
			for name, node := range d.Nodes {
				for _, kind := range []agent.Kind{"claude", "codex"} {
					opts, req, _ := deliveryFixture()
					opts.Tiers = config.Defaults().Tiers
					req.Agent = kind
					req.Mode = policy.Mode
					req.Workflow = policy.Workflow
					p, err := planWorkflowDelivery(opts, req, d, name, policy)
					if err != nil {
						t.Fatal(err)
					}
					delivery, err := writeDelivery(t.Context(), t.TempDir(), "bundle", p)
					if err != nil {
						t.Fatal(err)
					}
					request := agent.Request{Invocation: agent.InvocationGraphNode, Graph: &agent.GraphInvocation{Node: name}, Delivery: delivery}
					if err := agent.VerifyRequestDelivery(t.Context(), request); err != nil {
						t.Fatalf("%s/%s: %v", name, kind, err)
					}
					for path, raw := range node.Files {
						if strings.HasPrefix(path, "skills/team-guidance/") {
							native := filepath.Join(delivery.Root, "plugin", filepath.FromSlash(path))
							actual, err := os.ReadFile(native)
							if err != nil || !bytes.Equal(raw, actual) {
								t.Fatalf("%s/%s lost migrated resource %s: %v", name, kind, path, err)
							}
						}
					}
					if _, err := verifyDelivery(t.Context(), delivery.Root, p); err != nil {
						t.Fatal(err)
					}
					// The prerequisite launch boundary must reject a changed migrated resource.
					file := filepath.Join(delivery.Root, "plugin/skills/team-guidance/litefactory.md")
					if err := os.WriteFile(file, []byte("changed migrated guidance"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := agent.VerifyRequestDelivery(t.Context(), request); err == nil {
						t.Fatal("launch accepted changed migrated instructions")
					}
				}
			}
		})
	}
}

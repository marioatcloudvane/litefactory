package teamsource

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestSelectedStackGuidanceDeliveredToBothProvidersAndColdReplay(t *testing.T) {
	registry := planPackageRegistry(t)
	definition, err := capturePlan(t.Context(), t, registry)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"synthetic-stack","skill":"plan/skills/non-go/SKILL.md"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := CaptureStackGuidance(t.Context(), stackGuidanceFixture(), resolved.Effective)
	if err != nil {
		t.Fatal(err)
	}
	if err := AttachStackProfile(t.Context(), definition, resolved, strings.Repeat("a", 40), guidance); err != nil {
		t.Fatal(err)
	}
	replay, err := CaptureWorkflowIdentity(t.Context(), RetainedFiles(definition.Files), "plan.dot", definition.Profile.Team, agent.WorkflowPlan, registry)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Digest != replay.Digest || !reflect.DeepEqual(definition.Nodes, replay.Nodes) {
		t.Fatal("cold replay changed selected node delivery or digest")
	}
	for id, node := range definition.Nodes {
		for _, provider := range []string{"claude", "codex"} {
			files, err := NativeWorkflow(provider, node, "model", []string{"Read"}, "read-only")
			if err != nil {
				t.Fatal(id, provider, err)
			}
			got := map[string][]byte{}
			for _, file := range files {
				got[file.Path] = file.Bytes
			}
			for name, raw := range guidance {
				_, relative, _ := strings.Cut(name, "/skills/")
				parts := strings.Split(relative, "/")
				// Dependencies retain their nested reader ID but native paths use basename.
				if strings.HasPrefix(relative, "shared/base/") {
					parts = append([]string{"base"}, parts[2:]...)
				}
				native := "plugin/skills/" + strings.Join(parts, "/")
				if !bytes.Equal(got[native], raw) {
					t.Fatalf("%s/%s did not receive exact %s", id, provider, native)
				}
			}
		}
	}
}

func TestStackGuidanceDeliveryCollisionPreservesAdmission(t *testing.T) {
	definition, resolved, guidance := capturedStackFixture(t)
	definition.Nodes = map[string]WorkflowNode{"role": {Skills: []string{"non-go"}, Files: map[string][]byte{"skills/non-go/SKILL.md": []byte("conflicting admitted bytes")}}}
	prior := definition.Nodes["role"].Files["skills/non-go/SKILL.md"]
	if err := AttachStackProfile(t.Context(), definition, resolved, strings.Repeat("a", 40), guidance); err == nil {
		t.Fatal("selected guidance overwrote graph skill authority")
	}
	if definition.Digest != "original" || definition.StackProfile != nil || !bytes.Equal(definition.Nodes["role"].Files["skills/non-go/SKILL.md"], prior) {
		t.Fatal("failed attachment changed existing admission")
	}
}

func TestStackGuidanceNativeDeliverySupportsPackageRootSkills(t *testing.T) {
	raw := []byte("---\nname: stack\ndescription: Package root guidance\n---\nAdvice.")
	nodes, err := stackDeliveryNodes(map[string]WorkflowNode{"role": {}}, map[string][]byte{"skills/stack/SKILL.md": raw})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes["role"].Skills, []string{"stack"}) || !bytes.Equal(nodes["role"].Files["skills/stack/SKILL.md"], raw) {
		t.Fatal("package root guidance missing from native delivery")
	}
}

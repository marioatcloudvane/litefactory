package teamsource

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The harness selects a node's role as "<plugin>:<role>", with the plugin name
// from agent.DeliveryPlugin and the role from the role's frontmatter. That name
// must exist in what the renderer actually produced for Claude: when the plugin
// was renamed and the harness kept the old prefix, every Claude graph node failed
// with "--agent ... not found" while all unit tests stayed green.
func TestClaudeDeliveryAddressesItsRenderedRole(t *testing.T) {
	t.Parallel()
	research, err := CaptureResearch(t.Context(), researchTestRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CapturePlan(t.Context(), planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]WorkflowNode{}
	for name, node := range research.Nodes {
		nodes["research/"+name] = node
	}
	for name, node := range plan.Nodes {
		nodes["plan/"+name] = node
	}
	checked := 0
	for name, node := range nodes {
		if node.Role.Frontmatter.Name == "" {
			continue
		}
		checked++
		files, err := NativeWorkflow("claude", node, "test-model", []string{"Read"}, "read-only")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rendered := map[string][]byte{}
		for _, f := range files {
			rendered[f.Path] = f.Bytes
		}
		var manifest struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rendered["plugin/.claude-plugin/plugin.json"], &manifest); err != nil {
			t.Fatalf("%s: plugin manifest: %v", name, err)
		}
		if manifest.Name != agent.DeliveryPlugin {
			t.Errorf("%s: plugin renders as %q, harness addresses %q", name, manifest.Name, agent.DeliveryPlugin)
		}
		role := node.Role.Frontmatter.Name
		if _, ok := rendered["plugin/agents/"+role+".md"]; !ok {
			t.Errorf("%s: harness asks for %s:%s but no plugin/agents/%s.md was rendered", name, agent.DeliveryPlugin, role, role)
		}
	}
	if checked == 0 {
		t.Fatal("no role-bearing Research or Plan node was checked")
	}
}

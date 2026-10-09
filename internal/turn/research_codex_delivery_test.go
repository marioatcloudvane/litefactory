package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestResearchCodexDeliveryRetainsCompleteMainInstructions(t *testing.T) {
	t.Parallel()
	opts, req, _ := deliveryFixture()
	req.Agent = "codex"
	opts.Tiers.ModelByTier["codex"] = map[config.Tier]string{"balanced": "worker"}
	node := teamsource.ResearchNode{Role: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "research", Description: "Research"}, Body: "MAIN_ROLE"}, Task: "Investigate current sources", Tier: "balanced", Skills: []string{"evidence"}, Files: map[string][]byte{"skills/evidence/SKILL.md": []byte("---\nname: evidence\ndescription: Evidence\n---\nRead evidence")}}
	def := &teamsource.ResearchDefinition{Profile: &graphexec.Profile{}, Digest: "source", Nodes: map[string]teamsource.ResearchNode{"first": node, "second": node}}
	p, err := planResearchDelivery(opts, req, def, "first")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MAIN_ROLE", "Investigate current sources", researchNodeInstructions, "Ordinary project guidance"} {
		if !strings.Contains(string(p.files["main-instructions.md"]), want) {
			t.Errorf("retained developer instructions omit %q", want)
		}
	}
	if _, ok := p.files["plugin/skills/evidence/SKILL.md"]; !ok {
		t.Fatal("selected native skill absent")
	}
	for _, name := range []string{"first", "second"} {
		next, err := planResearchDelivery(opts, req, def, name)
		if err != nil || (next.identity == p.identity) != (name == "first") {
			t.Fatalf("incorrect same/different-node identity: %s %v", name, err)
		}
	}
	node.Role.Body = "UPDATED_MAIN_ROLE"
	def.Nodes["first"] = node
	next, err := planResearchDelivery(opts, req, def, "first")
	if err != nil || next.identity == p.identity {
		t.Fatalf("updated developer instructions did not rotate session identity: %v", err)
	}
	delivery, err := writeDelivery(t.Context(), t.TempDir(), "bundles/codex", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyDelivery(t.Context(), delivery.Root, p); err != nil {
		t.Fatalf("retained source verification failed: %v", err)
	}
}

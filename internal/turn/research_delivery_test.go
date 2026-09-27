package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestResearchDeliveryIdentityAndNativeClosure(t *testing.T) {
	opts, req, _ := deliveryFixture()
	req.Agent = "claude"
	opts.Tiers.ModelByTier["claude"] = map[config.Tier]string{"balanced": "worker"}
	node := teamsource.ResearchNode{Role: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "research", Description: "Research"}, Body: "MAIN_ROLE"}, Task: "Investigate", Tier: "balanced", Skills: []string{"evidence"}, Files: map[string][]byte{"skills/evidence/SKILL.md": []byte("---\nname: evidence\ndescription: Evidence\n---\nRead evidence"), "research.md": []byte("MAIN_ROLE")}}
	def := &teamsource.ResearchDefinition{Profile: &graphexec.Profile{}, Digest: "source", Nodes: map[string]teamsource.ResearchNode{"first": node, "second": node}}
	p, err := planResearchDelivery(opts, req, def, "first")
	if err != nil {
		t.Fatal(err)
	}
	// Omitted role tools inherit the session's tools: the --tools ceiling still
	// bounds them, and a written list would hide the attempt's MCP tools.
	if header := strings.SplitN(string(p.files["plugin/agents/research.md"]), "---\n\n", 2)[0]; strings.Contains(header, "tools:") {
		t.Fatalf("omitted role tools rendered an allowlist instead of inheriting:\n%s", header)
	}
	if p.mainRole != "research" || p.mainModel != "worker" || len(p.roles) != 1 {
		t.Fatalf("main role not selected: %+v", p)
	}
	if _, ok := p.files["plugin/skills/evidence/SKILL.md"]; !ok {
		t.Fatal("missing selected native skill")
	}
	if !strings.Contains(string(p.files["bootstrap.md"]), "Do not print a JSON result") {
		t.Fatal("missing persistent node result context")
	}
	for _, name := range []string{"first", "second"} {
		next, err := planResearchDelivery(opts, req, def, name)
		if err != nil {
			t.Fatal(err)
		}
		if (next.identity == p.identity) != (name == "first") {
			t.Fatalf("node %s identity mismatch", name)
		}
	}
	def.Digest = "formatting-only-source-change"
	same, err := planResearchDelivery(opts, req, def, "first")
	if err != nil || same.identity != p.identity {
		t.Fatalf("source formatting rotated effective identity: %v", err)
	}
	node.Task = "Changed effective task"
	def.Nodes["first"] = node
	changed, err := planResearchDelivery(opts, req, def, "first")
	if err != nil || changed.identity == p.identity {
		t.Fatalf("changed task did not rotate identity: %v", err)
	}
	delivery, err := writeDelivery(t.Context(), t.TempDir(), "bundles/test", p)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.MainRole != "research" || delivery.MainModel != "worker" {
		t.Fatal("main role lost during retention")
	}
	retained, err := verifyDelivery(delivery.Root, p)
	if err != nil || retained.MainRole != "research" {
		t.Fatalf("main role lost during resume: %v", err)
	}
}

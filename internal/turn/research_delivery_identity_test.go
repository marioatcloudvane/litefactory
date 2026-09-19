package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestResearchDeliveryInstructionChangesRotateIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*teamsource.ResearchNode, *Request)
	}{
		{"role", func(n *teamsource.ResearchNode, _ *Request) { n.Role.Body = "Updated research instructions" }},
		{"skill", func(n *teamsource.ResearchNode, _ *Request) {
			n.Files["skills/evidence/SKILL.md"] = []byte("---\nname: evidence\ndescription: Evidence\n---\nUpdated evidence instructions")
		}},
		{"permissions", func(_ *teamsource.ResearchNode, r *Request) { r.Tools = []string{"Read", "Grep"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, req, _ := deliveryFixture()
			req.Agent = "claude"
			opts.Tiers.ModelByTier["claude"] = map[config.Tier]string{"balanced": "worker"}
			node := teamsource.ResearchNode{Role: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "research", Description: "Research"}, Body: "Investigate the request"}, Task: "Investigate", Tier: "balanced", Skills: []string{"evidence"}, Files: map[string][]byte{"skills/evidence/SKILL.md": []byte("---\nname: evidence\ndescription: Evidence\n---\nRead evidence")}}
			def := &teamsource.ResearchDefinition{Profile: &graphexec.Profile{}, Digest: "source", Nodes: map[string]teamsource.ResearchNode{"research": node}}
			before, err := planResearchDelivery(opts, req, def, "research")
			if err != nil {
				t.Fatal(err)
			}

			tc.change(&node, &req)
			def.Nodes["research"] = node
			after, err := planResearchDelivery(opts, req, def, "research")

			if err != nil {
				t.Fatal(err)
			}
			if before.identity == after.identity {
				t.Fatal("changed effective instructions retained the old resume identity")
			}
			if after.mainRole != "research" || after.mainModel != "worker" {
				t.Fatal("instruction rotation lost the selected main role or model")
			}
		})
	}
}

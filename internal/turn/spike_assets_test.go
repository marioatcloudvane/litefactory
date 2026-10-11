package turn

import (
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestSpikeShipsOneCopyOfEachRoleAndOneSkill(t *testing.T) {
	specs, err := SpikeSpecs()
	mustProgress(t, err)
	definition, err := teamsource.CaptureSpike(t.Context(), specs)
	mustProgress(t, err)
	var roles, skills, graphs []string
	for name := range definition.Files {
		switch {
		case path.Ext(name) == ".dot":
			graphs = append(graphs, name)
		case strings.HasPrefix(name, "skills/") && path.Base(name) == "SKILL.md":
			skills = append(skills, name)
		case !strings.Contains(name, "/") && path.Ext(name) == ".md":
			roles = append(roles, name)
		}
	}
	slices.Sort(roles)
	slices.Sort(skills)
	if len(graphs) != 1 || strings.Join(roles, ",") != "spike-fixer.md,spike-investigator.md" || strings.Join(skills, ",") != "skills/spike-result/SKILL.md" {
		t.Fatalf("shipped package: graphs=%v roles=%v skills=%v", graphs, roles, skills)
	}
	if len(definition.Nodes) != 2 {
		t.Fatalf("expected the investigator and the fixer, got %v", definition.Nodes)
	}
}

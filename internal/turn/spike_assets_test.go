package turn

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestSpikeShipsOneCopyOfEachRoleAndOneSkillNamingNoGraphNode(t *testing.T) {
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
	if len(graphs) != 1 || strings.Join(roles, ",") != "spike-fixer.md,spike-investigator.md" || len(skills) != 1 || skills[0] != "skills/spike-result/SKILL.md" {
		t.Fatalf("shipped package: graphs=%v roles=%v skills=%v", graphs, roles, skills)
	}
	if len(definition.Nodes) != 2 {
		t.Fatalf("expected the investigator and the fixer, got %v", definition.Nodes)
	}
	for name, raw := range definition.Files {
		if path.Ext(name) != ".md" {
			continue
		}
		text := string(raw)
		if strings.Contains(text, "lf_") || strings.Contains(text, "->") {
			t.Errorf("%s carries graph syntax", name)
		}
		for _, node := range definition.Profile.Nodes {
			if node.Terminal {
				continue
			}
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(node.Name) + `\b`).MatchString(text) {
				t.Errorf("%s names graph node %q", name, node.Name)
			}
		}
	}
	// Every role instruction describes the job, never the step that follows it.
	for _, name := range []string{"spike-investigator.md", "spike-fixer.md"} {
		role := string(definition.Files[name])
		for _, banned := range []string{"next step", "then the factory runs", "goes to"} {
			if strings.Contains(strings.ToLower(role), banned) {
				t.Errorf("%s describes flow: %q", name, banned)
			}
		}
	}
}

func TestSpikeSourceTreeHoldsOneCopyOfTheFlow(t *testing.T) {
	var found []string
	mustProgress(t, fs.WalkDir(osRepoFS(t), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "dist" || d.Name() == ".aiteam") {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.Contains(p, "spike") && (path.Ext(p) == ".dot" || strings.HasSuffix(p, "spike-investigator.md") || strings.HasSuffix(p, "spike-fixer.md")) {
			found = append(found, p)
		}
		return nil
	}))
	want := map[string]bool{"internal/teamsource/spike/spike.dot": true, "internal/teamsource/spike/spike-investigator.md": true, "internal/teamsource/spike/spike-fixer.md": true}
	if len(found) != len(want) {
		t.Fatalf("the flow or role exists more than once: %v", found)
	}
	for _, p := range found {
		if !want[p] {
			t.Fatalf("unexpected copy %s", p)
		}
	}
}

func osRepoFS(t *testing.T) fs.FS {
	t.Helper()
	return os.DirFS("../..")
}

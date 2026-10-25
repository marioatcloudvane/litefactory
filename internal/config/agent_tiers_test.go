package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The canonical roles declare capability tiers. Native delivery resolves those
// tiers through configuration, without a second literal-model host tree.
func TestAgentTiersMatchCanonicalRoleFrontmatter(t *testing.T) {
	roles, err := filepath.Glob(filepath.Join(moduleRoot(t), "teams", "source", "base-team", "*", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, file := range roles {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(raw), "---\n") {
			continue
		}
		parts := strings.SplitN(string(raw)[4:], "\n---", 2)
		if len(parts) != 2 {
			t.Fatalf("%s: missing frontmatter end", file)
		}
		var fm struct {
			Name string `yaml:"name"`
			Tier Tier   `yaml:"tier"`
		}
		if err := yaml.Unmarshal([]byte(parts[0]), &fm); err != nil {
			t.Fatal(err)
		}
		seen++
		if fm.Name == "" || !fm.Tier.Valid() {
			t.Errorf("%s: missing role name or invalid tier %q", file, fm.Tier)
		}
		for _, kind := range []Kind{KindClaude, KindCodex} {
			if model, ok := Defaults().Tiers.ResolveModel(kind, fm.Tier); !ok || model == "" {
				t.Errorf("%s: %s tier %s has no model", file, kind, fm.Tier)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no canonical roles checked")
	}
}

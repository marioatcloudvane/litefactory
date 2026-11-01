package graphexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Captured from the starting commit before extraction, for every shipped/source
// DOT graph and the retained legacy graph. These identities cover the factory
// wire representation, semantic identity and every declared navigation result.
func TestExtractionRetainsShippedAndLegacyGraphIdentities(t *testing.T) {
	type identity struct{ Profile, Semantic, Navigation string }
	raw, err := os.ReadFile("testdata/extraction-baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string]identity
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline) == 0 {
		t.Fatal("empty graph compatibility baseline")
	}
	for _, path := range slices.Sorted(maps.Keys(baseline)) {
		want := baseline[path]
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", path))
			if err != nil {
				t.Fatal(err)
			}
			p, err := ParseProfile(data)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(encoded)
			semantic, err := SemanticDigest(p)
			if err != nil {
				t.Fatal(err)
			}
			var next []string
			for _, node := range p.Nodes {
				for _, outcome := range node.Outcomes {
					target, ok := Decide(p, node.Name, outcome)
					next = append(next, fmt.Sprintf("%s/%s=%s/%t", node.Name, outcome, target, ok))
				}
			}
			encoded, err = json.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			nh := sha256.Sum256(encoded)
			got := identity{hex.EncodeToString(h[:]), semantic, hex.EncodeToString(nh[:])}
			if got != want {
				t.Fatalf("factory graph identity changed: got %+v, baseline %+v", got, want)
			}
		})
	}
}

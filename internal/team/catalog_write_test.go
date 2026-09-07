package team

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogBatchRefusesUnownedAndSymlinkTargetsBeforeWriting(t *testing.T) {
	for _, bad := range []string{"../escape", ".git/config", "CLAUDE.md", "agents/link/file.md"} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "agents"), 0o700); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(root, "agents/link")); err != nil {
				t.Fatal(err)
			}
			err := WriteCatalogUnlocked(t.Context(), root, map[string][]byte{"agents/good.md": []byte("good"), bad: []byte("bad")}, []byte("{}"))
			if err == nil {
				t.Fatal("bad target accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "agents/good.md")); !os.IsNotExist(err) {
				t.Fatal("partially wrote before preflight failure")
			}
			if _, err := os.Stat(filepath.Join(root, ".litefactory/catalog.json")); !os.IsNotExist(err) {
				t.Fatal("false provenance written")
			}
		})
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestDaemonStartupIgnoresAndPreservesRetiredTeamFiles(t *testing.T) {
	t.Parallel()
	paths := config.NewPaths(t.TempDir())
	if _, err := config.WriteDefault(paths); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"teams/catalog.json", "teams/overrides/private.md", "cache/teams/release.json", "config.yaml"} {
		path := filepath.Join(paths.Home(), name)
		if name != "config.yaml" {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("invalid retired data\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = raw
	}
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDaemonConfig(paths); err != nil {
		t.Fatal(err)
	}
	started, err := openDaemonDB(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := started.Close(); err != nil {
		t.Fatal(err)
	}
	for path, before := range files {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("retired operator file changed at %s: %v", path, err)
		}
	}
}

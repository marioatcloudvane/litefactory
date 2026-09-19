package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchExecutionStrictSelection(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		valid           bool
	}{
		{"graph", "execution: graph\n", "graph", true},
		{"legacy", "execution: legacy\n", "legacy", true},
		{"missing value", "execution:\n", "", false},
		{"unknown field", "execution: graph\nsource: other\n", "", false},
		{"duplicate", "execution: graph\nexecution: legacy\n", "", false},
		{"unknown path", "execution: future\n", "", false},
		{"second document", "execution: graph\n---\nexecution: legacy\n", "", false},
		{"empty", "", "", false},
		{"oversized", strings.Repeat(" ", 1025), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".litefactory")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ResearchExecution(root)
			if (err == nil) != tc.valid || got != tc.want {
				t.Fatalf("selection %q err=%v want %q valid=%v", got, err, tc.want, tc.valid)
			}
		})
	}
	t.Run("absent uses default", func(t *testing.T) {
		got, err := ResearchExecution(t.TempDir())
		if err != nil || got != "" {
			t.Fatalf("%q %v", got, err)
		}
	})
	t.Run("symlink refused", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Symlink(t.TempDir(), filepath.Join(root, ".litefactory")); err != nil {
			t.Fatal(err)
		}
		if _, err := ResearchExecution(root); err == nil {
			t.Fatal("accepted symlink")
		}
	})
}

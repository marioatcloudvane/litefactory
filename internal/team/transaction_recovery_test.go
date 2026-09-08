package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The journal schema is used only to construct the interrupted-process state.
// The expected outcome is AC-2.17: a lock holder sees a complete old generation,
// never a mix of partially published new files and old metadata.
func TestCatalogNextLockRecoversInterruptedPublication(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".litefactory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agents/shared"), 0700); err != nil {
		t.Fatal(err)
	}
	journal := catalogJournal{Version: 1, Entries: []catalogBackup{
		{Path: "index.json", Exists: true, Data: []byte("old index")},
		{Path: "agents/shared/reader.md", Exists: true, Data: []byte("old agent")},
		{Path: "agents/shared/new.md", Exists: false},
		{Path: ".litefactory/catalog.json", Exists: true, Data: []byte("old metadata")},
	}}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range map[string][]byte{"index.json": []byte("new index"), "agents/shared/reader.md": []byte("new agent"), "agents/shared/new.md": []byte("new only"), ".litefactory/catalog.json": []byte("old metadata"), ".litefactory/catalog-transaction.json": encoded} {
		if err := os.WriteFile(filepath.Join(root, p), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		err := WithCatalogLock(t.Context(), root, func() error {
			for p, want := range map[string]string{"index.json": "old index", "agents/shared/reader.md": "old agent", ".litefactory/catalog.json": "old metadata"} {
				got, err := os.ReadFile(filepath.Join(root, p))
				if err != nil || string(got) != want {
					t.Errorf("recovered %s = %q, %v; want %q", p, got, err, want)
				}
			}
			for _, p := range []string{"agents/shared/new.md", ".litefactory/catalog-transaction.json"} {
				if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
					t.Errorf("interrupted artifact %s retained: %v", p, err)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

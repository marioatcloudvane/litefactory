package teampackage

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/cloudvane/litefactory/internal/config"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestStablePackageCaptureRetainsOwnedBytesAndDetectsEdits(t *testing.T) {
	for _, action := range []string{"edit", "delete", "unchanged"} {
		t.Run(action, func(t *testing.T) {
			raw := []byte("original graph")
			sum := sha256.Sum256(raw)
			source := fstest.MapFS{"graph.dot": &fstest.MapFile{Data: raw}}
			capture := &stablePackageFiles{ctx: t.Context(), source: source, files: map[string][]byte{}, expected: map[string]string{"graph.dot": hex.EncodeToString(sum[:])}}
			copied, err := fs.ReadFile(capture, "graph.dot")
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "edit":
				source["graph.dot"].Data = []byte("edited graph")
			case "delete":
				delete(source, "graph.dot")
			}
			err = capture.verify()
			if action == "unchanged" && err != nil {
				t.Fatal(err)
			}
			if action != "unchanged" && err == nil {
				t.Fatal("mutation accepted during capture")
			}
			if string(copied) != "original graph" {
				t.Fatal("capture did not own original bytes")
			}
		})
	}
}

func TestStablePackageCaptureRefusesInventoryMismatch(t *testing.T) {
	capture := &stablePackageFiles{ctx: t.Context(), source: fstest.MapFS{"graph.dot": &fstest.MapFile{Data: []byte("changed")}}, files: map[string][]byte{}, expected: map[string]string{"graph.dot": "wrong"}}
	if _, err := fs.ReadFile(capture, "graph.dot"); err == nil {
		t.Fatal("corrupt fetched release accepted")
	}
}

func TestEditableSelectionAcceptsEditsWithoutChangingReceipt(t *testing.T) {
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	installFixture(t, m, source, "1.0.0")
	local, err := m.Clone(t.Context(), source, "example", "1.0.0", "custom", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(filepath.Dir(local.Path), "receipt.json")
	before, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first edit", "second edit"} {
		raw := []byte(text)
		if err := os.WriteFile(filepath.Join(local.Path, "research", "flow.dot"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		err := m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
			return a.WithSelected(t.Context(), "local", "custom", "2.0.0", func(entry Installed, files fs.FS) error {
				if !entry.Editable {
					t.Fatal("lost local identity")
				}
				got, err := fs.ReadFile(files, "research/flow.dot")
				if err != nil {
					return err
				}
				if string(got) != text {
					t.Fatal("stale local bytes captured")
				}
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(receipt)
	if err != nil || string(before) != string(after) {
		t.Fatal("capture rewrote origin receipt")
	}
}

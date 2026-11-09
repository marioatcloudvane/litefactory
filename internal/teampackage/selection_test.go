package teampackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestRemovalGuardRefusesBeforeMutation(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	entry := installFixture(t, m, source, "1.0.0")
	calls := 0
	m.inUse = func(_ context.Context, id Identity) ([]string, error) {
		calls++
		if id != entry.Identity {
			t.Fatal(id)
		}
		return []string{"/z/repo", "/a/repo"}, nil
	}
	rename := m.rename
	m.rename = func(string, string) error { t.Fatal("linked release detached"); return nil }
	removed, err := m.Remove(t.Context(), "", "example", "1.0.0")
	if removed || err == nil || !strings.Contains(err.Error(), "/a/repo, /z/repo") || calls != 1 {
		t.Fatal(removed, err, calls)
	}
	failure := errors.New("binding database unreadable")
	m.inUse = func(context.Context, Identity) ([]string, error) { return nil, failure }
	if removed, err = m.Remove(t.Context(), source, "example", "1.0.0"); removed || !errors.Is(err, failure) {
		t.Fatal(removed, err)
	}
	if _, err = m.Inspect(t.Context(), source, "example", "1.0.0"); err != nil {
		t.Fatal("guard altered installed version", err)
	}
	m.rename = rename
	m.inUse = func(context.Context, Identity) ([]string, error) { return nil, nil }
	if removed, err = m.Remove(t.Context(), source, "example", "1.0.0"); !removed || err != nil {
		t.Fatal(removed, err)
	}
}

func TestCatalogueSelectionValidatesBeforeCallbackAndExpires(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	entry := installFixture(t, m, source, "1.0.0")
	var retained *CatalogueAccess
	called := false
	err := m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
		retained = a
		return a.WithSelected(t.Context(), "", "example", "1.0.0", func(got Installed, files fs.FS) error {
			called = true
			if !reflect.DeepEqual(got, entry) {
				t.Fatal(got, entry)
			}
			if _, err := fs.ReadFile(files, "research/flow.dot"); err != nil {
				t.Fatal(err)
			}
			if _, err := fs.ReadFile(files, "../receipt.json"); err == nil {
				t.Fatal("escaped confined content")
			}
			return nil
		})
	})
	if err != nil || !called {
		t.Fatal(err, called)
	}
	if err := retained.WithSelected(t.Context(), source, "example", "1.0.0", func(Installed, fs.FS) error { return nil }); err == nil {
		t.Fatal("expired catalogue access accepted")
	}
	invalid := errors.New("invalid mapped workflow")
	m.validate = func(context.Context, fs.FS, map[string]string) error { return invalid }
	err = m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
		return a.WithSelected(t.Context(), source, "example", "1.0.0", func(Installed, fs.FS) error { t.Fatal("callback ran before validation"); return nil })
	})
	if !errors.Is(err, ErrIncompatibleWorkflow) || !errors.Is(err, invalid) {
		t.Fatal(err)
	}
}

func TestReadSelectedFileReadsInventoriedTeamFiles(t *testing.T) {
	t.Parallel()
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), nil)
	r, files := fixtureRelease("1.0.0")
	files["access/access-classifier.md"] = []byte("fixture access role\n")
	r.Files = nil
	for p, raw := range files {
		sum := sha256.Sum256(raw)
		r.Files = append(r.Files, File{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	slices.SortFunc(r.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	r.Checksum = digest(r.Files)
	entry, _, err := m.InstallBuilt(t.Context(), "/fixture/teams/source", files, r, BuildInfo{SourceHead: strings.Repeat("c", 40)})
	if err != nil {
		t.Fatal(err)
	}
	var retained *CatalogueAccess
	err = m.WithCatalogue(t.Context(), func(a *CatalogueAccess) error {
		retained = a
		got, raw, err := a.ReadSelectedFile(t.Context(), "", "example", "1.0.0", "access/access-classifier.md")
		if err != nil {
			return err
		}
		if got.Identity != entry.Identity || string(raw) != "fixture access role\n" {
			t.Fatalf("read %v %q", got.Identity, raw)
		}
		if _, _, err := a.ReadSelectedFile(t.Context(), "", "example", "1.0.0", "access/missing.md"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("missing file: %v", err)
		}
		if _, _, err := a.ReadSelectedFile(t.Context(), "", "example", "1.0.0", "../receipt.json"); err == nil {
			t.Fatal("escaped confined content")
		}
		if err := os.WriteFile(filepath.Join(entry.Path, "access", "access-classifier.md"), []byte("edited\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.ReadSelectedFile(t.Context(), "", "example", "1.0.0", "access/access-classifier.md"); err == nil {
			t.Fatal("changed sealed file accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := retained.ReadSelectedFile(t.Context(), "", "example", "1.0.0", "access/access-classifier.md"); err == nil {
		t.Fatal("expired catalogue access accepted")
	}
}

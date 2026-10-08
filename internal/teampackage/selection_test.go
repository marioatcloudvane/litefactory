package teampackage

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
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
	fixture, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	entry := installFixture(t, m, source, "1.0.0")
	fixture.mu.Lock()
	requests := fixture.requests
	fixture.mu.Unlock()
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
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if requests != fixture.requests {
		t.Fatal("selection accessed network")
	}
}

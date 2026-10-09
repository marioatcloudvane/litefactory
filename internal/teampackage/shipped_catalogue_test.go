package teampackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// TestShippedCatalogueMatchesItsFiles keeps teams/teams.json installable: every
// listed file exists with its hash, nothing unlisted ships, and each release
// preserves its checksummed immutable bytes. Only the canonical current release
// carries the same role and workflow bytes as the editable teamsource.
func TestShippedCatalogueMatchesItsFiles(t *testing.T) {
	raw, err := os.ReadFile("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := parseCatalogue(raw)
	if err != nil {
		t.Fatalf("teams.json does not parse: %v", err)
	}
	canonicalRaw, err := os.ReadFile("../teamsource/release.json")
	if err != nil {
		t.Fatal(err)
	}
	var canonical Release
	if err := json.Unmarshal(canonicalRaw, &canonical); err != nil {
		t.Fatal(err)
	}
	foundCanonical := false
	for _, r := range cat.Releases {
		if r.Slug == "standard" {
			t.Errorf("release %s: there is no standard team; name it after what it is", r.Version)
		}
		current := r.Slug == canonical.Slug && r.Version == canonical.Version
		if current {
			foundCanonical = true
		}
		root := filepath.Join("../../teams", filepath.FromSlash(r.Download))
		listed := map[string]bool{}
		for _, f := range r.Files {
			listed[f.Path] = true
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
			if err != nil {
				t.Errorf("%s: %v", f.Path, err)
				continue
			}
			if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != f.SHA256 {
				t.Errorf("%s/%s: listed hash is stale", r.Slug, f.Path)
			}
			if !current || f.Path == "team.config" || f.Path == "GENERATED.md" {
				continue
			}
			live, err := os.ReadFile(filepath.Join("../teamsource", filepath.FromSlash(f.Path)))
			if err != nil || string(live) != string(b) {
				t.Errorf("%s/%s differs from internal/teamsource/%s", r.Slug, f.Path, f.Path)
			}
		}
		if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			if rel = filepath.ToSlash(rel); !listed[rel] {
				t.Errorf("%s/%s ships but is not listed", r.Slug, rel)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !foundCanonical {
		t.Fatal("canonical current release missing from catalogue")
	}
}

// TestShippedTeamInstallsFromTheCatalogueFile installs the Go team the way an
// operator does for a private repository: from the checkout's catalogue file.
func TestShippedTeamInstallsFromTheCatalogueFile(t *testing.T) {
	catalogue, err := filepath.Abs("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: func(ctx context.Context, source fs.FS, workflows map[string]string) error {
		for _, graph := range workflows {
			if _, err := fs.ReadFile(source, graph); err != nil {
				return err
			}
		}
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, already, err := m.Install(t.Context(), catalogue, "go-development", "")
	if err != nil || already || !got.Integrity || got.Identity.Source != "file://"+filepath.ToSlash(catalogue) {
		t.Fatalf("install: %+v already=%v %v", got.Identity, already, err)
	}
	if len(got.Workflows) != 6 {
		t.Fatalf("workflows: %v", got.Workflows)
	}
	if _, again, err := m.Install(t.Context(), "file://"+filepath.ToSlash(catalogue), "go-development", got.Identity.Version); err != nil || !again {
		t.Fatalf("reinstall: already=%v %v", again, err)
	}
}

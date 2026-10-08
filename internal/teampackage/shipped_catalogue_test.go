package teampackage

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestShippedCatalogueMatchesItsFiles keeps teams/teams.json installable: every
// listed file exists with its hash, nothing unlisted ships, and each release
// carries the same role and workflow bytes as the embedded teamsource.
func TestShippedCatalogueMatchesItsFiles(t *testing.T) {
	raw, err := os.ReadFile("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := parseCatalogue(raw)
	if err != nil {
		t.Fatalf("teams.json does not parse: %v", err)
	}
	for _, r := range cat.Releases {
		if r.Slug == "standard" {
			t.Errorf("release %s: there is no standard team; name it after what it is", r.Version)
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
			if f.Path == "team.config" {
				continue
			}
			live, err := os.ReadFile(filepath.Join("../teamsource", filepath.FromSlash(f.Path)))
			if err != nil || string(live) != string(b) {
				t.Errorf("%s/%s differs from internal/teamsource/%s", r.Slug, f.Path, f.Path)
			}
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			if rel = filepath.ToSlash(rel); !listed[rel] && !strings.HasPrefix(path.Base(rel), ".") {
				t.Errorf("%s/%s ships but is not listed", r.Slug, rel)
			}
			return nil
		})
	}
}

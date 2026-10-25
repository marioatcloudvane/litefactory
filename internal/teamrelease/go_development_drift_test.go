package teamrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/teampackage"
)

// A team source may be ahead of its latest published release. Two things must
// hold: published release directories never change, and a release can be built
// from the current source.

// Every published release directory must still match the checksums recorded in
// the catalogue.
func TestPublishedReleasesAreUnchanged(t *testing.T) {
	raw, err := os.ReadFile("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := teampackage.DecodeCatalogue(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, release := range catalogue.Releases {
		dir := filepath.Join("../../teams", filepath.FromSlash(release.Download))
		for _, file := range release.Files {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
			if err != nil {
				t.Errorf("%s/%s: %v", release.Download, file.Path, err)
				continue
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != file.SHA256 {
				t.Errorf("%s/%s changed after publication", release.Download, file.Path)
			}
		}
		if got := len(snapshot(t, dir)); got != len(release.Files) {
			t.Errorf("%s has %d files, catalogue lists %d", release.Download, got, len(release.Files))
		}
	}
}

// buildFromSource builds the release of one real source, with its base, into a
// fresh repository copy that holds no earlier releases.
func buildFromSource(t *testing.T, source string, bases ...string) {
	t.Helper()
	root := canonicalTemp(t)
	for _, dir := range append([]string{source}, bases...) {
		for name, raw := range snapshot(t, filepath.Join("../..", dir)) {
			put(t, filepath.Join(root, dir, filepath.FromSlash(name)), raw)
		}
	}
	if err := GenerateSources(t.Context(), root, false, []string{source}); err != nil {
		t.Fatalf("build %s: %v", source, err)
	}
	if err := GenerateSources(t.Context(), root, true, []string{source}); err != nil {
		t.Fatalf("drift check of fresh build %s: %v", source, err)
	}
}

func TestGoDevelopmentSourceBuilds(t *testing.T) {
	buildFromSource(t, "teams/source/go-development", "teams/source/base-team")
}

func TestPythonFastAPISourceBuilds(t *testing.T) {
	buildFromSource(t, "teams/source/python-fastapi", "teams/source/base-team")
}

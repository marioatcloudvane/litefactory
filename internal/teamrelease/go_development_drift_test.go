package teamrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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

// Every published release must match its current source: a teams/source change
// lands together with its regenerated release and catalogue. Fix a failure with
// the all-teams go run ./cmd/teamrelease command in teams/README.md; never bump.
func TestPublishedReleasesMatchSource(t *testing.T) {
	root := canonicalTemp(t)
	for name, raw := range snapshot(t, "../../teams") {
		put(t, filepath.Join(root, "teams", filepath.FromSlash(name)), raw)
	}
	catalogue := releaseCatalogue(t, root)
	var sources []string
	for _, release := range catalogue.Releases {
		sources = append(sources, "teams/source/"+release.Slug)
	}
	if len(sources) == 0 {
		t.Fatal("catalogue lists no releases")
	}
	if err := GenerateSources(t.Context(), root, true, sources); err != nil {
		t.Fatalf("published releases differ from teams/source; regenerate them with go run ./cmd/teamrelease (see teams/README.md):\n%v", err)
	}
	// The same check catches a source change that was not regenerated.
	p := filepath.Join(root, "teams/source/base-team/bug/bug-fixer.md")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	put(t, p, append(raw, []byte("\nUnregenerated change.\n")...))
	if err := GenerateSources(t.Context(), root, true, sources); err == nil || !strings.Contains(err.Error(), "drift: teams/teams.json") {
		t.Fatalf("unregenerated source change was not reported as drift: %v", err)
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

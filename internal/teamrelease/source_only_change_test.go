package teamrelease

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A valid source-only change builds and installs without cutting a release or
// touching any published release directory or catalogue entry.
func TestSourceOnlyChangeBuildsWithoutReleaseCut(t *testing.T) {
	root := canonicalTemp(t)
	for name, raw := range snapshot(t, "../../teams") {
		put(t, filepath.Join(root, "teams", filepath.FromSlash(name)), raw)
	}
	prior := releaseCatalogue(t, root)
	history := make(map[string]map[string][]byte)
	for _, release := range prior.Releases {
		history[release.Download] = snapshot(t, filepath.Join(root, "teams", release.Download))
	}
	changes := []struct{ file, old, new string }{
		{"research/research.md", "Establish the facts, record what you could not settle, and hand over.", "Establish the facts, note what you could not settle, and hand over."},
		{"research/research.dot", ` brief -> stopped [lf_on="failed"];`, ` brief -> failed [lf_on="failed"];`},
	}
	for _, c := range changes {
		p := filepath.Join(root, "teams/source/base-team", c.file)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(raw, []byte(c.old)) != 1 {
			t.Fatalf("%s: expected exactly one %q", c.file, c.old)
		}
		put(t, p, bytes.Replace(raw, []byte(c.old), []byte(c.new), 1))
	}
	// This version exists only inside the test's temporary directory.
	const version = "9999.0.0"
	metaPath := filepath.Join(root, "teams/source/go-development/release.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	meta["version"], meta["download"] = version, "go-development/v"+version
	raw, err = json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	put(t, metaPath, raw)
	sources := []string{"teams/source/go-development"}
	if err := GenerateSources(t.Context(), root, false, sources); err != nil {
		t.Fatal(err)
	}
	if err := GenerateSources(t.Context(), root, true, sources); err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		got, err := os.ReadFile(filepath.Join(root, "teams/go-development/v"+version, c.file))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(got, []byte(c.new)) || bytes.Contains(got, []byte(c.old)) {
			t.Fatalf("candidate %s lacks the source change", c.file)
		}
	}
	after := releaseCatalogue(t, root)
	for _, old := range prior.Releases {
		if !reflect.DeepEqual(history[old.Download], snapshot(t, filepath.Join(root, "teams", old.Download))) {
			t.Fatalf("published directory changed: %s", old.Download)
		}
		found := false
		for _, got := range after.Releases {
			found = found || reflect.DeepEqual(old, got)
		}
		if !found {
			t.Fatalf("published catalogue entry changed: %s %s", old.Slug, old.Version)
		}
	}
	manager, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	installed, _, err := manager.Install(t.Context(), filepath.Join(root, "teams/teams.json"), "go-development", version)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Identity.Slug != "go-development" || installed.Identity.Version != version || len(installed.Workflows) != 6 {
		t.Fatal("changed source did not install all six workflows")
	}
}

package teamrelease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Source validation builds a candidate in a private fixture. It does not require
// the editable source to reproduce an already published, immutable version.
func TestCanonicalSourceAheadOfPublishedRelease(t *testing.T) {
	root := fixture(t)
	for name, raw := range snapshot(t, "../../teams") {
		// The fixture already holds the go source resolved to flat files; copying
		// the real child's inherit.json over it would make every file a collision.
		if strings.HasPrefix(name, "source/") {
			continue
		}
		put(t, filepath.Join(root, "teams", filepath.FromSlash(name)), raw)
	}
	prior := releaseCatalogue(t, root)
	history := make(map[string]map[string][]byte)
	for _, release := range prior.Releases {
		history[release.Download] = snapshot(t, filepath.Join(root, "teams", release.Download))
	}
	metaPath := filepath.Join(root, "teams/source/go-development/release.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	// This version exists only inside the test's temporary directory.
	const version = "9999.0.0"
	meta["version"], meta["download"] = version, "go-development/v"+version
	raw, err = json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	put(t, metaPath, raw)
	source := snapshot(t, filepath.Join(root, "teams/source/go-development"))
	generate(t, root)
	if err := Generate(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source, snapshot(t, filepath.Join(root, "teams/source/go-development"))) {
		t.Fatal("candidate build rewrote editable source")
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
			t.Fatalf("published catalogue identity changed: %s %s", old.Slug, old.Version)
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
	if len(installed.Workflows) != len(workflows) {
		t.Fatal("current source did not build a complete installable candidate")
	}
}

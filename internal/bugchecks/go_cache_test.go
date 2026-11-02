package bugchecks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestManagedCacheFinalEnvironmentAndRevision(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	caches, err := config.NewPaths(filepath.Join(root, "home")).ResolveGoBuildCaches(config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(root, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := (Isolation{PATH: "/usr/bin:/bin", Home: root, Revision: "v1", Environment: map[string]string{"GOCACHE": "/wrong"}}).WithGoBuildCaches(caches)
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision == "v1" || !slices.Contains(p.WritableRoots, caches.Main) || slices.Contains(p.WritableRoots, caches.Reviewer) {
		t.Fatalf("isolation contract: %+v", p)
	}
	p.invocationEnv = []string{"GOCACHE=/invocation-override"}
	_, _, env, err := p.resolve(tree, config.BugCheckCommand{Argv: []string{"/bin/echo"}, CWD: ".", Env: []string{"GOCACHE"}})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOCACHE=") {
			count++
			if entry != "GOCACHE="+caches.Main {
				t.Fatalf("cache override survived: %v", env)
			}
		}
	}
	if count != 1 {
		t.Fatalf("cache occurrences %d: %v", count, env)
	}
	p.Environment["GOCACHEPROG"] = "external-helper"
	if _, _, _, err := p.resolve(tree, config.BugCheckCommand{Argv: []string{"/bin/echo"}, CWD: "."}); err == nil {
		t.Fatal("external cache helper accepted")
	}
}

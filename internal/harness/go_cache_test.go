package harness

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestManagedCacheFinalClaudeEnvironment(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.Tools = []string{"Read", "Bash"}
	})
	result, err := f.runner.Run(t.Context(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded() {
		t.Fatalf("run: %+v", result)
	}
	env := f.dump(t, "env.dump")
	if !strings.Contains(env, "GOCACHE="+f.runner.goBuildCaches.Main+"\n") || strings.Contains(env, "GOCACHE="+f.runner.goBuildCaches.Reviewer+"\n") {
		t.Fatalf("final cache environment: %s", env)
	}
	if !strings.Contains(f.dump(t, "argv.dump"), ".settings.json") {
		t.Fatal("the run's sandboxed settings were not selected")
	}
}

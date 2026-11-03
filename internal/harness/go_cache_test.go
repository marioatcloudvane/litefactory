package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestMainCacheSettingsPreservePolicy(t *testing.T) {
	f := newFixture(t, "", func(c *config.Claude) {
		c.Tools = []string{"Read", "Bash"}
		c.AllowedTools = []string{"Read", "Bash(go test:*)"}
	})
	if err := os.WriteFile(f.runner.settingsPath, []byte(`{"sandbox":{"enabled":true,"filesystem":{"allowWrite":["/operator-cache"],"denyRead":["/secret"]},"network":{"deniedDomains":["*"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(f.home, "run")
	selected, err := f.runner.prepareMainCacheSettings(f.worktree, logPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(selected)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{f.runner.goBuildCaches.Main, "/operator-cache", "/secret", "deniedDomains"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("policy missing %s: %s", want, data)
		}
	}
	if strings.Contains(string(data), f.runner.goBuildCaches.Reviewer) {
		t.Fatal("main run granted reviewer cache")
	}
}

func TestManagedCacheFinalClaudeEnvironment(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.Tools = []string{"Read", "Bash"}
		c.AllowedTools = []string{"Read", "Bash(go test:*)"}
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
		t.Fatal("main cache settings were not selected")
	}
}

func TestMainCacheSettingsRejectMalformedPolicy(t *testing.T) {
	f := newFixture(t, "")
	for name, raw := range map[string]string{
		"null document":      `null`,
		"null sandbox":       `{"sandbox":null}`,
		"non-object sandbox": `{"sandbox":[]}`,
		"null filesystem":    `{"sandbox":{"filesystem":null}}`,
		"non-array grants":   `{"sandbox":{"filesystem":{"allowWrite":"/cache"}}}`,
		"non-string grant":   `{"sandbox":{"filesystem":{"allowWrite":[true]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(f.runner.settingsPath, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			selected, err := f.runner.prepareMainCacheSettings(f.worktree, filepath.Join(f.home, "run"))
			if err == nil || selected != "" {
				t.Fatalf("malformed settings accepted: selected=%q err=%v", selected, err)
			}
		})
	}
}

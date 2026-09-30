package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeExecution(t *testing.T, root, file, raw string) {
	t.Helper()
	dir := filepath.Join(root, ".litefactory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

// A leftover .litefactory/spike.yaml is inert: Research's own setting is read
// exactly as before whatever the retired file holds, including invalid content.
func TestLeftoverSpikeSettingDoesNotAffectResearchSetting(t *testing.T) {
	for _, raw := range []string{"execution: legacy\n", "execution: graph\n", "garbage: [\n", ""} {
		root := t.TempDir()
		writeExecution(t, root, "research.yaml", "execution: graph\n")
		writeExecution(t, root, "spike.yaml", raw)
		if got, err := ResearchExecution(root); err != nil || got != "graph" {
			t.Fatalf("spike.yaml %q changed Research: %q %v", raw, got, err)
		}
	}
}

func TestSpikeRoleGrantsAreExplicitAndValidated(t *testing.T) {
	profiles := DefaultToolProfiles().Permissions
	inv, fix := profiles[SpikeInvestigatorProfile], profiles[SpikeFixerProfile]
	if inv.Codex.Sandbox != "read-only" || fix.Codex.Sandbox != "workspace-write" || inv.Attachments || fix.Attachments {
		t.Fatalf("unexpected default Spike grants: %+v %+v", inv, fix)
	}
	for _, tool := range inv.Claude.Tools {
		if tool == "Edit" || tool == "Write" || tool == "MultiEdit" || tool == "Bash" {
			t.Fatalf("investigator grant includes %s", tool)
		}
	}
	implement := profiles["implement"]
	for _, tool := range fix.Claude.Tools {
		if tool == "Bash" || tool == "Task" {
			t.Fatalf("fixer grant is not the narrow role surface: %s in %v (implement has %v)", tool, fix.Claude.Tools, implement.Claude.Tools)
		}
	}
}

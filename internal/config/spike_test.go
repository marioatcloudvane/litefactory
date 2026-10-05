package config

import (
	"testing"
)

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

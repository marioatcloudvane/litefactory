package config

import (
	"slices"
	"testing"
)

func TestSpikeRoleGrantsAreExplicitAndValidated(t *testing.T) {
	profiles := DefaultToolProfiles().Permissions
	inv, fix := profiles[SpikeInvestigatorProfile], profiles[SpikeFixerProfile]
	if inv.Codex.Sandbox != "read-only" || fix.Codex.Sandbox != "workspace-write" || inv.Attachments || fix.Attachments {
		t.Fatalf("unexpected default Spike grants: %+v %+v", inv, fix)
	}
	for _, tool := range inv.Claude.Tools {
		if tool == "Edit" || tool == "Write" || tool == "MultiEdit" {
			t.Fatalf("investigator grant includes %s", tool)
		}
	}
	// The investigator runs commands through the read-only shell: Bash with
	// the fixed read-only rules, which include the project checks.
	if !slices.Contains(inv.Claude.Tools, "Bash") || !slices.Contains(inv.Claude.AllowedTools, "Bash(go test:*)") {
		t.Fatalf("investigator cannot run the checks: %v %v", inv.Claude.Tools, inv.Claude.AllowedTools)
	}
	for _, rule := range inv.Claude.AllowedTools {
		if ClaudeToolName(rule) == "Bash" && !slices.Contains(ValidateReviewerShellRules(), rule) {
			t.Fatalf("investigator rule %q is outside the read-only shell", rule)
		}
	}
	// The fixer runs the build and tests like Implement, without delegation.
	implement := profiles["implement"]
	if !slices.Contains(fix.Claude.Tools, "Bash") || slices.Contains(fix.Claude.Tools, "Task") {
		t.Fatalf("fixer grant is not the contained engineering surface: %v (implement has %v)", fix.Claude.Tools, implement.Claude.Tools)
	}
	for _, rule := range fix.Claude.AllowedTools {
		if ClaudeToolName(rule) == "Bash" && !slices.Contains(implement.Claude.AllowedTools, rule) {
			t.Fatalf("fixer rule %q exceeds implement", rule)
		}
	}
	for _, want := range []string{"Bash(go test:*)", "Bash(go vet:*)", "Bash(go build:*)"} {
		if !slices.Contains(fix.Claude.AllowedTools, want) {
			t.Fatalf("fixer cannot run %s: %v", want, fix.Claude.AllowedTools)
		}
	}
	cfg := Defaults()
	cfg.Agents.Claude = &Claude{}
	cfg.Agents.Codex = &Codex{}
	for _, name := range []string{SpikeInvestigatorProfile, SpikeFixerProfile} {
		if errs := validateBugRoleProfile(&cfg, name, profiles[name]); len(errs) != 0 {
			t.Fatalf("default %s grant refused: %v", name, errs)
		}
	}
}

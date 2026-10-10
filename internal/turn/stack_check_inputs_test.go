package turn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func checkInputProfile(t *testing.T, argv, cwd, phase string) *config.ResolvedStackProfile {
	t.Helper()
	raw := `{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"` + phase + `":[{"name":"check","argv":["` + argv + `"],"cwd":"` + cwd + `","timeout_seconds":10}]}`
	p, err := config.ResolveStackProfile([]byte(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &p
}
func TestReadOnlyCheckGuidanceWithoutMCPObservesInputsWithoutExecution(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "recording-check")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch executed-canary\n"), 0700); err != nil {
		t.Fatal(err)
	}
	req := agent.Request{Mode: "plan", WorktreePath: root, StackProfile: checkInputProfile(t, "./recording-check", ".", "checks")}
	if err := validateStackCommandInputs(Options{}, req); err != nil {
		t.Fatal("read-only guidance check refused without MCP", err)
	}
	if _, err := os.Stat(filepath.Join(root, "executed-canary")); !os.IsNotExist(err) {
		t.Fatal("admission executed a check", err)
	}
	req.StackProfile = checkInputProfile(t, "go", ".", "checks")
	if err := validateStackCommandInputs(Options{}, req); err != nil {
		t.Fatal("Go profile checks require no MCP executor when used as guidance", err)
	}
	req.StackProfile = checkInputProfile(t, "unavailable-stack-tool", ".", "checks")
	if err := validateStackCommandInputs(Options{}, req); err == nil {
		t.Fatal("unavailable executable admitted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	req.StackProfile = checkInputProfile(t, "go", "escape", "checks")
	if err := validateStackCommandInputs(Options{}, req); err == nil {
		t.Fatal("escaping CWD admitted")
	}
}
func TestReadOnlySetupWithoutMCPNeverExecutes(t *testing.T) {
	root := t.TempDir()
	req := agent.Request{Mode: "validate", WorktreePath: root, StackProfile: checkInputProfile(t, "go", ".", "setup")}
	if err := validateStackCommandInputs(Options{}, req); err == nil {
		t.Fatal("setup admitted without operator isolation")
	}
	results, err := runStackSetup(t.Context(), Options{}, req, map[string]string{"lf_access": "read", "lf_agent_markdown": "story-worker"})
	if err != nil || len(results) != 0 {
		t.Fatal("read-only Validate attempted setup", results, err)
	}
}

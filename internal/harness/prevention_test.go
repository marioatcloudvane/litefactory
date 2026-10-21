package harness

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"path/filepath"
	"slices"
	"testing"
)

func TestPreventionBrokerOnlyArgumentsAndLowerTurnLimit(t *testing.T) {
	dir := t.TempDir()
	spec, err := agent.NewMCPLaunchSpec("/usr/bin/true", filepath.Join(dir, "descriptor.json"), "analysis-attempt", agent.PreventionToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	req := agent.DoctorRequest{Attempt: "analysis-attempt", Model: "fixture", Prompt: "data on stdin", WorkingDirectory: dir, MCPLaunch: spec, TurnLimit: 7}
	args, err := scopedDoctorArguments(req, filepath.Join(dir, "mcp.json"), "prevention")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"--max-turns": "7", "--tools": "", "--setting-sources": "", "--permission-mode": "dontAsk"} {
		i := slices.Index(args, key)
		if i < 0 || i+1 == len(args) || args[i+1] != want {
			t.Fatalf("%s wrong boundary: %v", key, args)
		}
	}
	for _, flag := range []string{"--strict-mcp-config", "--no-session-persistence", "--restricted"} {
		if !slices.Contains(args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	if slices.Contains(args, req.Prompt) {
		t.Fatal("source instructions entered argv")
	}
	req.TurnLimit = 25
	if _, err = scopedDoctorArguments(req, "/tmp/mcp.json", "prevention"); err == nil {
		t.Fatal("unbounded turns accepted")
	}
	wrong, err := agent.NewMCPLaunchSpec("/usr/bin/true", filepath.Join(dir, "descriptor.json"), "analysis-attempt", agent.DoctorToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	req.MCPLaunch = wrong
	req.TurnLimit = 7
	if _, err = scopedDoctorArguments(req, "/tmp/mcp.json", "prevention"); err == nil {
		t.Fatal("diagnosis SQL tools crossed advisory boundary")
	}
}

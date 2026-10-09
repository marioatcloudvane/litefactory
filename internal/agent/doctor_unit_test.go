package agent_test

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"path/filepath"
	"testing"
)

func TestDoctorExactCatalogAndRequiredSandbox(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name  string
		tools []string
		valid bool
	}{
		{"exact", agent.DoctorToolNames(), true},
		{"missing", agent.DoctorToolNames()[:4], false},
		{"old communication only", agent.DoctorToolNames()[:3], false},
		{"extra", append(agent.DoctorToolNames(), "issues_comment"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec, err := agent.NewMCPLaunchSpec("/fixture/factory", filepath.Join(dir, "descriptor.json"), "attempt", test.tools...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "diagnose", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credentials"}}}
			if err := request.Validate(); (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
			request.Sandbox = nil
			if err := request.Validate(); err == nil {
				t.Fatal("missing sandbox admitted")
			}
		})
	}
}

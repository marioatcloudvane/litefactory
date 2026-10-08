package codex

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestDoctorProfileMapsScratchCredentialsNetworkAndClosedGate(t *testing.T) {
	dir := t.TempDir()
	spec, err := agent.NewMCPLaunchSpec("/fixture/factory", filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	profile := &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/factory/config.yaml", "/factory/secrets", "/operator/.codex", "/operator/.ssh", "/operator/.config/gh"}}
	request := agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "inspect", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: profile}
	runner := &Runner{}
	args, err := runner.doctorArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(args, " ")
	for _, want := range []string{"features.shell_tool=true", "approval_policy=\"never\"", "default_permissions=\"doctor_diagnosis\"", "permissions.doctor_diagnosis.network.enabled=false", "permissions.doctor_diagnosis.filesystem.\"/\"=\"read\"", "permissions.doctor_diagnosis.filesystem." + strconv.Quote(dir) + "=\"write\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	for _, path := range profile.DeniedReadPaths {
		if !strings.Contains(text, "permissions.doctor_diagnosis.filesystem."+strconv.Quote(path)+"=\"deny\"") {
			t.Fatalf("credential path readable: %s", text)
		}
	}
	if strings.Contains(text, "workspace-write") || strings.Contains(text, "network.enabled=true") {
		t.Fatal("doctor uses broad writable/network profile")
	}
	if _, err := runner.DoctorIsolation(t.Context()); !errors.Is(err, agent.ErrDoctorIsolationUnverified) {
		t.Fatalf("gate opened: %v", err)
	}
	request.Sandbox = nil
	if _, err := runner.doctorArguments(request); err == nil {
		t.Fatal("sandbox omission accepted")
	}
}

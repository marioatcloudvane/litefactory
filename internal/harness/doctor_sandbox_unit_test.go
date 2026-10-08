package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestDoctorArgumentsRequireSandboxAndOnlyBash(t *testing.T) {
	dir := t.TempDir()
	spec, err := agent.NewMCPLaunchSpec("/fixture/factory", filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	profile := &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/factory/config.yaml", "/factory/secrets", "/operator/.claude", "/operator/.ssh", "/operator/.config/gh"}}
	request := agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "inspect", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: profile}
	args, err := doctorArguments(request, filepath.Join(dir, "private-provider", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "Bash" {
		t.Fatalf("native tool catalog: %v", args)
	}
	var settings struct {
		Sandbox struct {
			Enabled     bool                                     `json:"enabled"`
			Fail        bool                                     `json:"failIfUnavailable"`
			Unsandboxed bool                                     `json:"allowUnsandboxedCommands"`
			Excluded    []string                                 `json:"excludedCommands"`
			Filesystem  struct{ Writes, Denied, Reads []string } `json:"-"`
		} `json:"sandbox"`
	}
	i := slices.Index(args, "--settings")
	if i < 0 {
		t.Fatal("sandbox settings missing")
	}
	if err := json.Unmarshal([]byte(args[i+1]), &settings); err != nil {
		t.Fatal(err)
	}
	if !settings.Sandbox.Enabled || !settings.Sandbox.Fail || settings.Sandbox.Unsandboxed || len(settings.Sandbox.Excluded) != 0 {
		t.Fatalf("sandbox permits fallback: %s", args[i+1])
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(args[i+1]), &raw); err != nil {
		t.Fatal(err)
	}
	// Inspect encoded policy rather than inferring it from the profile name.
	var encoded struct {
		Sandbox struct {
			Filesystem struct {
				AllowWrite []string `json:"allowWrite"`
				DenyRead   []string `json:"denyRead"`
				DenyWrite  []string `json:"denyWrite"`
			} `json:"filesystem"`
			Network struct {
				Allowed []string `json:"allowedDomains"`
				Denied  []string `json:"deniedDomains"`
				Unix    bool     `json:"allowAllUnixSockets"`
				Local   bool     `json:"allowLocalBinding"`
			} `json:"network"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal([]byte(args[i+1]), &encoded); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(encoded.Sandbox.Filesystem.AllowWrite, []string{dir}) || len(encoded.Sandbox.Network.Allowed) != 0 || !slices.Equal(encoded.Sandbox.Network.Denied, []string{"*"}) || encoded.Sandbox.Network.Unix || encoded.Sandbox.Network.Local {
		t.Fatalf("writable/network ceiling widened: %s", args[i+1])
	}
	for _, path := range append(profile.DeniedReadPaths, filepath.Join(dir, "private-provider")) {
		if !slices.Contains(encoded.Sandbox.Filesystem.DenyRead, path) {
			t.Fatalf("credential readable: %s", path)
		}
	}
	if len(encoded.Sandbox.Filesystem.DenyWrite) != 2 || !strings.Contains(encoded.Sandbox.Filesystem.DenyWrite[0], "claude-") {
		t.Fatal("system temp fallback writable")
	}
	request.Sandbox = nil
	if _, err := doctorArguments(request, "/fixture/mcp.json"); err == nil {
		t.Fatal("sandbox omission accepted")
	}
	// Missing policy refuses before the native fixture could create a marker.
	marker := filepath.Join(dir, "launched")
	fixture := filepath.Join(dir, "provider")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Runner{}).RunDoctor(t.Context(), request); err == nil {
		t.Fatal("launch without profile succeeded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("missing sandbox launched native process")
	}
}

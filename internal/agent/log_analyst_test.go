package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLogAnalystRequestAuthority(t *testing.T) {
	dir := t.TempDir()
	spec, err := NewMCPLaunchSpec(filepath.Join(dir, "factory"), filepath.Join(dir, "descriptor.json"), "analysis", LogAnalystToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	r := LogAnalystRequest{Attempt: "analysis", Model: "model", Prompt: "task", WorkingDirectory: dir, MCPLaunch: spec}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Prompt = strings.Repeat("x", (64<<10)+1)
	if r.Validate() == nil {
		t.Fatal("accepted excessive bootstrap")
	}
	r.Prompt = "task"
	r.Attempt = "another"
	if r.Validate() == nil {
		t.Fatal("accepted another invocation's broker")
	}
	r.Attempt = "analysis"
	r.MCPLaunch, err = NewMCPLaunchSpec(filepath.Join(dir, "factory"), filepath.Join(dir, "descriptor.json"), "analysis", append(LogAnalystToolNames(), "shell")...)
	if err != nil {
		t.Fatal(err)
	}
	if r.Validate() == nil {
		t.Fatal("accepted broadened catalog")
	}
}

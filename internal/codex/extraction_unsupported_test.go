package codex

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestExtractionRetainsUnsupportedAuxiliaryRefusals(t *testing.T) {
	// A nil receiver makes accidental delegation to ordinary execution visible.
	// Neither unsupported operation needs provider configuration or credentials.
	var runner *Runner
	directory := t.TempDir()
	if _, err := runner.RunChat(t.Context(), agent.ChatRequest{Prompt: "context", Model: "model", WorkingDirectory: directory}); !errors.Is(err, agent.ErrDoctorIsolationUnverified) {
		t.Fatalf("chat isolation refusal changed: %v", err)
	}
	launch, err := agent.NewMCPLaunchSpec("/bin/sh", filepath.Join(directory, "descriptor.json"), "attempt", agent.LogAnalystToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunLogAnalyst(t.Context(), agent.LogAnalystRequest{Attempt: "attempt", Prompt: "context", Model: "model", WorkingDirectory: directory, MCPLaunch: launch}); !errors.Is(err, agent.ErrLogAnalystIsolation) {
		t.Fatalf("log analyst isolation refusal changed: %v", err)
	}
}

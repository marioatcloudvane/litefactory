package codex

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoctorRepairTransportUsesExactScopedToolsAndOpenGate(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	script := `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"repair-thread"}' '{"type":"turn.completed","usage":{"input_tokens":4,"output_tokens":2}}' 
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "repair-attempt", agent.RepairToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	var events []agent.UsageEvent
	request := agent.RepairRequest{Operation: "repair-operation", Invocation: agent.DoctorRequest{Attempt: "repair-attempt", Model: "configured-flagship", Prompt: "approved scope", WorkingDirectory: dir, UsageAttemptID: "repair-meter", MCPLaunch: spec, Usage: &agent.UsageCapture{AttemptID: "repair-meter", Sink: func(_ context.Context, e agent.UsageEvent) error { events = append(events, e); return nil }}}}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin"}}
	result, err := runner.RunDoctorRepair(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil || result.ObservedModel != "" {
		t.Fatalf("native usage/model: %+v", result)
	}
	args, err := runner.scopedDoctorArguments(request.Invocation, request.Operation)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, tool := range agent.RepairToolNames() {
		if !strings.Contains(joined, tool) {
			t.Errorf("missing scoped tool %s", tool)
		}
	}
	if _, err := runner.runDoctorTransport(t.Context(), request.Invocation); err == nil {
		t.Fatal("repair request launched through diagnosis path")
	}
}

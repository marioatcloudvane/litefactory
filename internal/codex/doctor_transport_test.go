package codex

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoctorNativeTransportKeepsUnknownModelAndInclusiveUsage(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	script := `#!/bin/sh
[ -z "$GH_TOKEN$LF_HOME$OPENAI_BASE_URL$CODEX_API_KEY" ] || exit 9
[ "$HOME" = "$CODEX_HOME" ] || exit 10
[ "$GIT_OPTIONAL_LOCKS" = "0" ] || exit 14
read -r prompt
[ "$prompt" = "prepared context" ] || exit 11
printf '%s\n' '{"type":"thread.started","thread_id":"thread"}' '{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":3}}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "doctor-attempt", agent.DoctorToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	var events []agent.UsageEvent
	request := agent.DoctorRequest{Attempt: "doctor-attempt", Model: "configured-model", Prompt: "prepared context\n", WorkingDirectory: dir, UsageAttemptID: "usage-attempt", MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credential-fixture"}}, Usage: &agent.UsageCapture{AttemptID: "usage-attempt", Sink: func(_ context.Context, event agent.UsageEvent) error { events = append(events, event); return nil }}}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=secret", "LF_HOME=unrelated", "OPENAI_BASE_URL=untrusted", "CODEX_API_KEY=fixture-secret"}}
	if _, err = runner.RunDoctor(t.Context(), request); !errors.Is(err, agent.ErrDoctorIsolationUnverified) {
		t.Fatalf("gate: %v", err)
	}
	if len(events) != 0 {
		t.Fatal("unverified adapter launched")
	}
	result, err := runner.runDoctorTransport(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservedModel != "" || result.ProviderVersion != "" || result.Usage == nil || result.Usage.Status != agent.CollectionPartial {
		t.Fatalf("result: %+v", result)
	}
	observations := 0
	for _, event := range events {
		if event.Observation != nil {
			observations++
			if event.Observation.ActualModel != nil || event.Observation.Scope != agent.UsageInclusive {
				t.Fatalf("invented exclusive model usage: %+v", event)
			}
		}
	}
	if observations != 1 {
		t.Fatalf("observations: %d", observations)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch cleanup: %v %v", entries, err)
	}
	args, err := runner.doctorArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--strict-config", "--ignore-user-config", "--ignore-rules", "--ephemeral", "features.shell_tool=true", "features.multi_agent=false", "communication_receipt"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("missing %s", flag)
		}
	}
	// Explicit provider failure stays sticky even if a later terminal event says success.
	if err := os.WriteFile(binary, []byte(script+`printf '%s\n' '{"type":"turn.failed","error":{"message":"fixture failure"}}' '{"type":"turn.completed","usage":{"input_tokens":0,"output_tokens":0}}'
`), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.runDoctorTransport(t.Context(), request); err == nil {
		t.Fatal("terminal failure was erased by later success")
	}

}

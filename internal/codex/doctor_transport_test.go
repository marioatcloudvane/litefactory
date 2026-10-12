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
	operatorHome := t.TempDir()
	auth := filepath.Join(operatorHome, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(auth), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
[ -z "$GH_TOKEN$LF_HOME$OPENAI_BASE_URL$CODEX_API_KEY" ] || exit 9
[ "$HOME" = "$CODEX_HOME" ] || exit 10
[ "$GIT_OPTIONAL_LOCKS" = "0" ] || exit 14
[ "$(readlink "$CODEX_HOME/auth.json")" = "__AUTH__" ] || exit 15
read -r prompt
[ "$prompt" = "prepared context" ] || exit 11
printf '%s\n' '{"type":"thread.started","thread_id":"thread"}' '{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":3}}'
`
	script = strings.ReplaceAll(script, "__AUTH__", auth)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "doctor-attempt", agent.DoctorToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	var events []agent.UsageEvent
	request := agent.DoctorRequest{Attempt: "doctor-attempt", Model: "configured-model", Prompt: "prepared context\n", WorkingDirectory: dir, UsageAttemptID: "usage-attempt", MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credential-fixture"}}, Usage: &agent.UsageCapture{AttemptID: "usage-attempt", Sink: func(_ context.Context, event agent.UsageEvent) error { events = append(events, event); return nil }}}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "HOME=" + operatorHome, "GH_TOKEN=secret", "LF_HOME=unrelated", "OPENAI_BASE_URL=untrusted", "CODEX_API_KEY=fixture-secret"}}
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
	if _, err := runner.runDoctorTransport(t.Context(), request); err == nil || !strings.Contains(err.Error(), "fixture failure") {
		t.Fatalf("terminal failure detail was lost or erased by later success: %v", err)
	}
	if _, err := os.Lstat(auth); err != nil {
		t.Fatalf("operator auth removed by cleanup: %v", err)
	}
	// Without an operator auth.json the launch proceeds and links nothing.
	noAuth := strings.Replace(script, `[ "$(readlink "$CODEX_HOME/auth.json")" = "`+auth+`" ] || exit 15`, `[ ! -e "$CODEX_HOME/auth.json" ] || exit 16`, 1)
	if err := os.WriteFile(binary, []byte(noAuth), 0700); err != nil {
		t.Fatal(err)
	}
	runner.environ = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	if _, err := runner.runDoctorTransport(t.Context(), request); err != nil {
		t.Fatalf("launch without operator auth: %v", err)
	}

}

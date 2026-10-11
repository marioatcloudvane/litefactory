package harness

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDoctorNativeTransportRetainsUsageAndIsolatesEnvironment(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	// A local fixture asserts stdin/environment; this is not a provider observation.
	script := `#!/bin/sh
[ -z "$GH_TOKEN$LF_HOME$ANTHROPIC_BASE_URL$ANTHROPIC_API_KEY" ] || exit 9
[ "$HOME" = "$CLAUDE_CONFIG_DIR" ] || exit 10
[ "$GIT_OPTIONAL_LOCKS" = "0" ] || exit 14
[ "$CLAUDE_CODE_SUBPROCESS_ENV_SCRUB" = "1" ] || exit 15
[ -L "$CLAUDE_CONFIG_DIR/.credentials.json" ] || exit 12
read -r prompt
[ "$prompt" = "prepared context" ] || exit 11
printf '%s\n' '{"type":"system","subtype":"init","session_id":"s","claude_code_version":"fixture-version"}' '{"type":"assistant","session_id":"s","message":{"id":"m","model":"observed-model","usage":{"input_tokens":10,"output_tokens":3}}}' '{"type":"result","session_id":"s","subtype":"success"}'
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
	osHome := t.TempDir()
	credentials := filepath.Join(osHome, ".claude", ".credentials.json")
	if err = os.MkdirAll(filepath.Dir(credentials), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(credentials, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{binary: config.Executable{Path: binary}, osHome: osHome, timeout: 10 * time.Second, forwardEnv: true, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=secret", "LF_HOME=unrelated", "ANTHROPIC_BASE_URL=untrusted", "ANTHROPIC_API_KEY=fixture-secret"}}
	if evidence, gateErr := runner.DoctorIsolation(t.Context()); gateErr != nil || evidence == "" {
		t.Fatalf("gate: %q %v", evidence, gateErr)
	}
	result, err := runner.RunDoctor(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(credentials); err != nil {
		t.Fatalf("scratch cleanup removed the operator's credentials: %v", err)
	}
	if result.ObservedModel != "observed-model" || result.ProviderVersion != "fixture-version" || result.Usage == nil || result.Usage.Status != agent.CollectionComplete || len(result.Usage.Diagnostics) != 0 {
		t.Fatalf("result: %+v", result)
	}
	observations := 0
	for _, event := range events {
		if event.Observation != nil {
			observations++
			if event.Observation.ActualModel == nil || *event.Observation.ActualModel != "observed-model" {
				t.Fatalf("model: %+v", event)
			}
		}
	}
	if observations != 1 {
		t.Fatalf("observations: %d", observations)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("transport scratch cleanup: %v %v", entries, err)
	}
	args, err := doctorArguments(request, "/private/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	tools := slices.Index(args, "--tools")
	if tools < 0 || args[tools+1] != "Bash" {
		t.Fatalf("native tool catalog differs from Bash only: %v", args)
	}
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--restricted", "--disable-slash-commands", "--strict-mcp-config", "mcp__litefactory__communication_receipt"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("missing %s", flag)
		}
	}
	// --bare refuses OAuth; subscription login must keep working.
	if slices.Contains(args, "--bare") {
		t.Errorf("--bare present: %v", args)
	}
	// Explicit provider failure stays sticky even if a later terminal event says success.
	if err := os.WriteFile(binary, []byte(script+`printf '%s\n' '{"type":"result","session_id":"s","is_error":true}' '{"type":"result","session_id":"s","is_error":false}'
`), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.runDoctorTransport(t.Context(), request); err == nil {
		t.Fatal("terminal failure was erased by later success")
	}

}

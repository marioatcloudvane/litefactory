package harness

import (
	"context"
	"errors"
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
[ -z "$GH_TOKEN$LF_HOME$ANTHROPIC_BASE_URL" ] || exit 9
[ "$HOME" = "$CLAUDE_CONFIG_DIR" ] || exit 10
read -r prompt
[ "$prompt" = "prepared context" ] || exit 11
printf '%s\n' '{"type":"system","subtype":"init","session_id":"s","claude_code_version":"fixture-version"}' '{"type":"assistant","session_id":"s","message":{"id":"m","model":"observed-model","usage":{"input_tokens":10,"output_tokens":3}}}' '{"type":"result","session_id":"s","subtype":"success"}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "doctor-attempt", "communication_preflight", "communication_submit", "communication_receipt")
	if err != nil {
		t.Fatal(err)
	}
	var events []agent.UsageEvent
	request := agent.DoctorRequest{Attempt: "doctor-attempt", Model: "configured-model", Prompt: "prepared context\n", WorkingDirectory: dir, UsageAttemptID: "usage-attempt", MCPLaunch: spec, Usage: &agent.UsageCapture{AttemptID: "usage-attempt", Sink: func(_ context.Context, event agent.UsageEvent) error { events = append(events, event); return nil }}}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=secret", "LF_HOME=unrelated", "ANTHROPIC_BASE_URL=untrusted"}}
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
	if result.ObservedModel != "observed-model" || result.ProviderVersion != "fixture-version" || result.Usage == nil || result.Usage.Status != agent.CollectionPartial {
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
	if tools < 0 || args[tools+1] != "" {
		t.Fatalf("native tools not empty: %v", args)
	}
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--bare", "--restricted", "--disable-slash-commands", "--strict-mcp-config", "mcp__litefactory__communication_receipt"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("missing %s", flag)
		}
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

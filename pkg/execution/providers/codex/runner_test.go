package codex

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

func TestArgumentsPreserveNativeOptions(t *testing.T) {
	got := Arguments(Options{Permission: &Sandbox{Mode: "workspace-write"}, ExtraArgs: []string{"-c", "mcp_servers={}"}, WritableRoots: []string{"/scratch"}, NetworkAccess: true, Model: "native", Effort: "high", ReasoningSummary: "concise"}, "thread-123")
	want := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "workspace-write", "-c", "mcp_servers={}", "--add-dir", "/scratch", "-c", "sandbox_workspace_write.network_access=true", "--model", "native", "-c", `model_reasoning_effort="high"`, "-c", `model_reasoning_summary="concise"`, "resume", "thread-123", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments: %#v", got)
	}
	if got := Arguments(Options{}, ""); strings.Contains(strings.Join(got, " "), "--sandbox") {
		t.Fatalf("omitted sandbox became explicit: %v", got)
	}
}

func TestPreparedRunNativeEvidenceAndSessionAcknowledgment(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "codex-fake")
	script := `#!/bin/sh
/bin/cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"thread-native"}' '{"type":"turn.started"}' '{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}' '{"type":"turn.completed","usage":{"input_tokens":0,"output_tokens":3}}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{Binary: binary, Environment: []string{"PATH=/usr/bin:/bin"}, Permission: &Sandbox{Mode: "read-only"}})
	if err != nil {
		t.Fatal(err)
	}
	launches, sessions := 0, 0
	var eventLog bytes.Buffer
	req := agent.Request{Events: &eventLog, Prompt: "prepared input", WorkingDirectory: t.TempDir(), Timeout: 10 * time.Second, BeforeLaunch: func(context.Context) error { launches++; return nil }, OnSession: func(_ context.Context, s string) error {
		sessions++
		if s != "thread-native" {
			t.Fatalf("session %q", s)
		}
		return nil
	}}
	got, err := r.Run(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Started || !got.Terminal || got.IsError || got.Text != "answer" || got.Session != "thread-native" || len(got.Events) != 4 || len(got.Usage) != 1 || launches != 1 || sessions != 1 {
		t.Fatalf("native evidence %+v, hooks %d/%d", got, launches, sessions)
	}
	if strings.Count(eventLog.String(), "\n") != 4 || !strings.HasSuffix(eventLog.String(), "\n") {
		t.Fatalf("native event framing lost: %q", eventLog.String())
	}
	refused := errors.New("session journal unavailable")
	req.OnSession = func(context.Context, string) error { return refused }
	got, err = r.Run(t.Context(), req)
	if !errors.Is(err, refused) || !got.Started || got.Text != "" || got.Terminal {
		t.Fatalf("unacknowledged answer: %+v %v", got, err)
	}
	refusal := errors.New("capture authorization refused")
	req.BeforeLaunch = func(context.Context) error { return refusal }
	got, err = r.Run(t.Context(), req)
	if !errors.Is(err, refusal) || got.Started {
		t.Fatalf("unauthorized launch: %+v %v", got, err)
	}
}

func TestNativeStreamFailureAndMalformedEvidence(t *testing.T) {
	data := `{"type":"thread.started","thread_id":"native"}
not json
{"type":"error","message":"recoverable notice"}
{"type":"turn.failed","error":{"message":"capacity exhausted","status_code":429}}
`
	var raw strings.Builder
	s := Consume(strings.NewReader(data), &raw, nil)
	if !s.Failed || s.Completed || s.Malformed != 1 || s.Events != 4 || s.FailureDetail != "capacity exhausted HTTP 429" || raw.String() != data {
		t.Fatalf("stream %+v, raw %q", s, raw.String())
	}
}

func TestPermissionPresenceAndEnvironmentIsolation(t *testing.T) {
	binary := "/bin/sh"
	if _, err := New(Options{Binary: binary, Permission: &Sandbox{}}); err == nil {
		t.Fatal("explicit empty sandbox accepted")
	}
	r, err := New(Options{Binary: binary, Permission: &Sandbox{Mode: "read-only"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.options.Permission.Mode != "read-only" {
		t.Fatal("sandbox lost")
	}
	got := Environment([]string{"HOME=/private-home", "PATH=/bin", "CODEX_API_KEY=secret", "LF_HOME=/factory", "OTHER=secret"})
	if !reflect.DeepEqual(got, []string{"PATH=/bin", "HOME=/private-home"}) {
		t.Fatalf("environment leaked: %v", got)
	}
}

func TestPreparedOutputBoundAndEmptyEnvironment(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "codex-fake")
	script := `#!/bin/sh
if test "${CODEX_TEST_SECRET-unset}" != unset; then exit 9; fi
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"a long answer that exceeds the configured line bound"}}' '{"type":"turn.completed"}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_TEST_SECRET", "must-not-inherit")
	r, err := New(Options{Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(t.Context(), agent.Request{Prompt: "prepared", WorkingDirectory: t.TempDir(), Timeout: 10 * time.Second, Output: agent.OutputPolicy{LineBytes: 32}})
	var failure *agent.Failure
	if !errors.As(err, &failure) || failure.Phase != "output" || !result.Started || len(result.Events) != 0 {
		t.Fatalf("output bound or isolation failed: %+v %v", result, err)
	}
}

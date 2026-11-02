package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

func nativeFixture(t *testing.T, script string, options Options) *Runner {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	options.Binary = binary
	runner, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestPreparedRunPreservesNativeEvidenceAndSessionAcknowledgment(t *testing.T) {
	runner := nativeFixture(t, `printf '%s\n' '{"type":"system","subtype":"init","session_id":"native-session","model":"native-model","tools":[]}' '{"type":"result","session_id":"native-session","is_error":false,"result":"done","usage":{"input_tokens":0,"output_tokens":2},"total_cost_usd":0}'`, Options{Tools: []string{}})
	launches, acknowledgments := 0, 0
	result, err := runner.Run(t.Context(), agent.Request{Prompt: "prepared", WorkingDirectory: t.TempDir(), Timeout: 10 * time.Second, BeforeLaunch: func(context.Context) error { launches++; return nil }, OnSession: func(_ context.Context, session string) error {
		if session != "native-session" {
			t.Fatalf("session %q", session)
		}
		acknowledgments++
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if launches != 1 || acknowledgments != 1 || !result.Started || !result.Terminal || result.IsError || result.ExitCode != 0 || result.Text != "done" || result.Session != "native-session" || len(result.Events) != 2 || len(result.Usage) != 1 || !strings.Contains(string(result.Usage[0]), `"input_tokens":0`) {
		t.Fatalf("native evidence: %#v launches=%d acknowledgments=%d", result, launches, acknowledgments)
	}
}

func TestPreparedRunRefusesLaunchAndSessionAcknowledgment(t *testing.T) {
	sentinel := errors.New("caller refused")
	runner := nativeFixture(t, `printf '%s\n' '{"type":"result","session_id":"native","is_error":false,"result":"must remain unaccepted"}'`, Options{})
	for _, phase := range []string{"launch", "session"} {
		t.Run(phase, func(t *testing.T) {
			request := agent.Request{Prompt: "prepared", WorkingDirectory: t.TempDir(), Timeout: 10 * time.Second}
			if phase == "launch" {
				request.BeforeLaunch = func(context.Context) error { return sentinel }
			} else {
				request.OnSession = func(context.Context, string) error { return sentinel }
			}
			result, err := runner.Run(t.Context(), request)
			var failure *agent.Failure
			if !errors.Is(err, sentinel) || !errors.As(err, &failure) || failure.Phase != map[string]string{"launch": "before_launch", "session": "session"}[phase] || result.Started != (phase == "session") || result.Terminal || result.Text != "" {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestArgumentsPreserveExplicitEmptyGrantAndResume(t *testing.T) {
	absent := Arguments(Options{}, "")
	empty := Arguments(Options{Tools: []string{}, Resume: true, Model: "native"}, "exact-session")
	if slices.Contains(absent, "--tools") {
		t.Fatal("omitted tools acquired a flag")
	}
	index := slices.Index(empty, "--tools")
	if index < 0 || empty[index+1] != "" {
		t.Fatalf("explicit empty grant lost: %q", empty)
	}
	index = slices.Index(empty, "--resume")
	if index < 0 || empty[index+1] != "exact-session" {
		t.Fatalf("exact session lost: %q", empty)
	}
}

func TestPreparedRunUsesCallerOutputLimitsAndEmptyEnvironment(t *testing.T) {
	t.Setenv("LF_EXTRACTION_SECRET", "ambient")
	runner := nativeFixture(t, `test -z "$LF_EXTRACTION_SECRET" || exit 19
printf '%s\n' '{"type":"result","result":"this terminal result exceeds the per-call limit"}'`, Options{})
	result, err := runner.Run(t.Context(), agent.Request{Prompt: "prepared", WorkingDirectory: t.TempDir(), Timeout: 10 * time.Second, Output: agent.OutputPolicy{LineBytes: 32, TotalBytes: 128}})
	if err == nil || !result.Started || result.Terminal || result.Text != "" {
		t.Fatalf("output bound: %#v %v", result, err)
	}
}

func TestExecuteRechecksAuthorizationOnRecoveredLaunch(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "claude")
	saved := binary + ".saved"
	sentinel := errors.New("authorization changed")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	restored := make(chan error, 1)
	capture, err := Execute(t.Context(), Prepared{Binary: binary, Directory: dir, BeforeLaunch: func(context.Context) error {
		calls++
		if calls == 1 {
			if err := os.Rename(binary, saved); err != nil {
				return err
			}
			go func() { time.Sleep(200 * time.Millisecond); restored <- os.Rename(saved, binary) }()
			return nil
		}
		return sentinel
	}})
	if restoreErr := <-restored; restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if calls != 2 || capture.Started || !errors.Is(err, sentinel) {
		t.Fatalf("calls=%d capture=%#v error=%v", calls, capture, err)
	}
}

func TestPreparedRunCollectsQualifiedNativeUsage(t *testing.T) {
	runner := nativeFixture(t, `printf '%s\n' '{"type":"system","subtype":"init","session_id":"native-session","model":"actual"}' '{"type":"assistant","session_id":"native-session","message":{"id":"m1","model":"actual","usage":{"input_tokens":0,"output_tokens":2}}}' '{"type":"result","session_id":"native-session","is_error":false,"result":"done"}'`, Options{})
	var observations []agent.UsageObservation
	result, err := runner.Run(t.Context(), agent.Request{Prompt: "prepared", WorkingDirectory: t.TempDir(), Timeout: 10 * time.Second, Usage: &agent.UsageCapture{Sink: func(_ context.Context, event agent.UsageEvent) error {
		if event.Observation != nil {
			observations = append(observations, *event.Observation)
		}
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].SourceID != "native-session" || observations[0].ActualModel == nil || *observations[0].ActualModel != "actual" || observations[0].Tokens.InputUncached == nil || *observations[0].Tokens.InputUncached != 0 || observations[0].Tokens.CacheRead != nil || result.Coverage == nil || result.Coverage.Status != agent.CollectionPartial {
		t.Fatalf("qualified usage: %#v coverage=%#v", observations, result.Coverage)
	}
	if len(result.Usage) != 1 || !strings.Contains(string(result.Usage[0]), `"session_id":"native-session"`) {
		t.Fatalf("raw source evidence: %s", result.Usage)
	}
}

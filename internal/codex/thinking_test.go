package codex

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestThinkingLogsMapToCodexReasoningSummary(t *testing.T) {
	for _, tt := range []struct {
		level config.ThinkingLogs
		want  string
	}{
		{config.ThinkingLogsNone, ""},
		{"", ""},
		{config.ThinkingLogsSummarized, `-c model_reasoning_summary="concise"`},
		{config.ThinkingLogsFull, `-c model_reasoning_summary="detailed"`},
	} {
		t.Run(string(tt.level), func(t *testing.T) {
			r, argvPath := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
			r.thinkingLogs = tt.level
			if _, err := r.Run(context.Background(), request(t, agent.PathCold)); err != nil {
				t.Fatalf("Run: %v", err)
			}
			run := strings.Join(strings.Fields(string(mustRead(t, argvPath))), " ")

			dir := t.TempDir()
			spec, err := agent.NewMCPLaunchSpec("/fixture/factory", filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			sandbox := &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/factory/config.yaml", "/factory/secrets", "/operator/.codex", "/operator/.ssh", "/operator/.config/gh"}}
			args, err := r.doctorArguments(agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "inspect", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: sandbox})
			if err != nil {
				t.Fatal(err)
			}
			if args[len(args)-1] != "-" {
				t.Fatalf("doctor argv does not end with the stdin prompt argument: %q", args)
			}
			doctor := strings.Join(args, " ")

			for name, argv := range map[string]string{"run": run, "doctor": doctor} {
				if tt.want == "" && strings.Contains(argv, "model_reasoning_summary") {
					t.Errorf("%s argv carries a reasoning summary for %q: %s", name, tt.level, argv)
				}
				if tt.want != "" && !strings.Contains(argv, tt.want) {
					t.Errorf("%s argv %s missing %s", name, argv, tt.want)
				}
			}
			if !strings.HasSuffix(run, "-") {
				t.Errorf("run argv %q does not end with the stdin prompt argument", run)
			}
		})
	}
}

func TestThinkingLogsLeaveTheCodexConversationUnchanged(t *testing.T) {
	args := strings.Join(conversationArguments(agent.ConversationRequest{Model: "gpt-test", Prompt: "hi"}, t.TempDir()), " ")
	if strings.Contains(args, "model_reasoning_summary") {
		t.Fatalf("conversation argv carries a reasoning summary: %s", args)
	}
}

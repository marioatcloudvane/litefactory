package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func judgeDenyRequest(t *testing.T, f *fixture) agent.JudgeRequest {
	t.Helper()
	launch, err := agent.NewMCPLaunchSpec(f.runner.binary.Path, filepath.Join(f.worktree, "descriptor.json"), "judge-deny-attempt", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	return agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-deny-attempt", Model: "opus", Prompt: "review saved work", WorkingDirectory: f.worktree, MCPLaunch: launch}}
}

func TestSelectedClaudeJudgeSettingsReachReadOnlyTransport(t *testing.T) {
	script := `printf '%s\n' "$@" > judge-argv.dump
while [ "$#" -gt 0 ]; do
 if [ "$1" = "--settings" ]; then shift; /bin/cat "$1" > judge-settings.dump; printf '%s' "$1" > judge-settings-path.dump; fi
 shift
done
printf '%s\n' '{"type":"result","is_error":false}'
`
	f := newFixture(t, script)
	req := judgeDenyRequest(t, f)
	req.DenyPolicy = testDenyPolicy(t, "Read(private/**)", "Agent(Explore)")
	if _, err := f.runner.RunJudge(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct{ Deny, Allow []string }
	}
	if err := json.Unmarshal([]byte(f.dump(t, "judge-settings.dump")), &settings); err != nil {
		t.Fatal(err)
	}
	for _, deny := range []string{"Bash(git push:*)", "Read(/" + filepath.ToSlash(f.worktree) + "/**/private/**)", "Agent(Explore)"} {
		if !slices.Contains(settings.Permissions.Deny, deny) {
			t.Errorf("judge settings lack %q", deny)
		}
	}
	if len(settings.Permissions.Allow) > 0 {
		t.Fatalf("judge settings gained allow rules: %v", settings.Permissions.Allow)
	}
	args := strings.Split(strings.TrimSpace(f.dump(t, "judge-argv.dump")), "\n")
	index := slices.Index(args, "--tools")
	if index < 0 || index+1 >= len(args) || args[index+1] != "Read,Grep,Glob" {
		t.Fatalf("judge tool ceiling changed: %v", args)
	}
	if _, err := os.Stat(f.dump(t, "judge-settings-path.dump")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("judge settings not cleaned: %v", err)
	}
}

func TestSelectedClaudeJudgeRefusesBeforeCapabilityProbe(t *testing.T) {
	f := newFixture(t, "touch started\n")
	req := judgeDenyRequest(t, f)
	req.Thinking = "high"
	req.DenyPolicy = testDenyPolicy(t, "Agent(model:opus)")
	if _, err := f.runner.RunJudge(t.Context(), req); err == nil || !strings.Contains(err.Error(), "cannot enforce") {
		t.Fatalf("unsupported judge policy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("judge probe or agent started: %v", err)
	}
}

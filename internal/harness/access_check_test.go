package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/naming"
)

const accessCheckFixtureRole = "FIXTURE ACCESS ROLE\n"

func accessCheckFixtureRequest(role string) agent.AccessCheckRequest {
	return agent.AccessCheckRequest{
		Role:       role,
		GrantText:  "fixture grant",
		Target:     json.RawMessage(`{"repo":"acme/acme-api","issue":123}`),
		IssueTitle: "fixture title",
		IssueBody:  "fixture body",
		Text:       "fixture comment",
		Command:    "/implement",
	}
}

func TestAccessCheckIsToolFreeStatelessAndParsesTheAnswer(t *testing.T) {
	answer := `{"type":"result","is_error":false,"result":"{\"decision\":\"allow\",\"reason\":\"fixture grant covers the target\"}"}`
	f := newFixture(t, dumpPrologue+emit(t, `{"type":"system","subtype":"init","tools":[]}`)+emit(t, answer))
	result, err := f.runner.CheckAccess(t.Context(), accessCheckFixtureRequest(accessCheckFixtureRole))
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	want := agent.AccessCheckResult{Decision: agent.AccessAllow, Reason: "fixture grant covers the target", Model: config.DefaultClaudeModel}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	argv := homeDump(t, f, "argv.dump")
	for _, flag := range []string{"--tools", "--strict-mcp-config"} {
		if !strings.Contains(argv, flag) {
			t.Errorf("argv %q missing %q", argv, flag)
		}
	}
	if strings.Contains(argv, "--session-id") || strings.Contains(argv, "--resume") {
		t.Errorf("access check argv carries a session: %q", argv)
	}
	prompt := homeDump(t, f, "stdin.dump")
	if !strings.HasPrefix(prompt, accessCheckFixtureRole) || !strings.Contains(prompt, "fixture grant") || !strings.Contains(prompt, "fixture comment") {
		t.Fatalf("prompt = %q", prompt)
	}
	path, err := naming.RunLogPath(f.home, testRef, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("access check created a run log %q", path)
	}
}

func TestAccessCheckUsesDefaultRoleWhenBlank(t *testing.T) {
	answer := `{"type":"result","is_error":false,"result":"{\"decision\":\"deny\",\"reason\":\"outside the grant\"}"}`
	f := newFixture(t, dumpPrologue+emit(t, `{"type":"system","subtype":"init","tools":[]}`)+emit(t, answer))
	pkg, err := instructionpackage.Load(t.Context(), fstest.MapFS{
		"metadata.yaml":               {Data: []byte("schema: 1\nversion: fixture\n")},
		"catalogue.yaml":              {Data: []byte("- access-classifier\n")},
		"agents/access-classifier.md": {Data: []byte("FIXTURE DEFAULT ACCESS ROLE\n")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	var captured []string
	f.runner.instructions = advisorCaptureFunc(func(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
		captured = append(captured, role)
		return pkg.Capture(ctx, role)
	})
	result, err := f.runner.CheckAccess(t.Context(), accessCheckFixtureRequest(""))
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if result.Decision != agent.AccessDeny || result.Reason != "outside the grant" {
		t.Fatalf("result = %+v", result)
	}
	if len(captured) != 1 || captured[0] != "access-classifier" {
		t.Fatalf("captured roles = %v", captured)
	}
	if prompt := homeDump(t, f, "stdin.dump"); !strings.HasPrefix(prompt, "FIXTURE DEFAULT ACCESS ROLE\n") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestAccessCheckRejectsToolsAndBadAnswers(t *testing.T) {
	good := `{"type":"result","is_error":false,"result":"{\"decision\":\"allow\",\"reason\":\"ok\"}"}`
	for _, tc := range []struct {
		name, init, result, want string
	}{
		{"tool exposure", `{"type":"system","subtype":"init","tools":["Bash"]}`, good, "exposed tools"},
		{"error result", `{"type":"system","subtype":"init","tools":[]}`, `{"type":"result","is_error":true,"result":"{\"decision\":\"allow\",\"reason\":\"ok\"}"}`, "no successful terminal result"},
		{"bad answer", `{"type":"system","subtype":"init","tools":[]}`, `{"type":"result","is_error":false,"result":"{\"decision\":\"maybe\",\"reason\":\"ok\"}"}`, "invalid answer"},
		{"fenced answer", `{"type":"system","subtype":"init","tools":[]}`, `{"type":"result","is_error":false,"result":"` + "```" + `json\n{\"decision\":\"allow\",\"reason\":\"ok\"}\n` + "```" + `"}`, "invalid answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, dumpPrologue+emit(t, tc.init)+emit(t, tc.result))
			result, err := f.runner.CheckAccess(t.Context(), accessCheckFixtureRequest(accessCheckFixtureRole))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if result != (agent.AccessCheckResult{}) {
				t.Fatalf("partial result on error: %+v", result)
			}
		})
	}
}

func TestAccessCheckUsesItsOwnTimeout(t *testing.T) {
	f := newFixture(t, "exec /bin/sleep 5\n", func(c *config.Claude) { c.TimeoutSeconds = 1 })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	_, err := f.runner.CheckAccess(ctx, accessCheckFixtureRequest(accessCheckFixtureRole))
	if err == nil {
		t.Fatal("CheckAccess succeeded against a hung provider")
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Fatalf("access check stopped after %s; it must not use the runner's 1s timeout", elapsed)
	}
}

// homeDump reads a file the fake claude wrote in its working directory, which
// for a stateless check is $LF_HOME rather than a worktree.
func homeDump(t *testing.T, f *fixture, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.home, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

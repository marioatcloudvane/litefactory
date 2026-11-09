package codex

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

func accessCheckFixtureRequest(role string) agent.AccessCheckRequest {
	return agent.AccessCheckRequest{
		Role:       role,
		GrantText:  "fixture grant",
		Target:     json.RawMessage(`{"repo":"acme/api","issue":1}`),
		IssueTitle: "fixture title",
		IssueBody:  "fixture body",
		Text:       "fixture comment",
		Command:    "/implement",
	}
}

func accessCheckScript(answer string) string {
	return `/bin/cat > stdin.dump
printf '%s\n' '{"type":"thread.started","thread_id":"access-thread"}'
printf '%s\n' '{"type":"turn.started"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":` + answer + `}}'
printf '%s\n' '{"type":"turn.completed"}'
`
}

func TestAccessCheckIsReadOnlyStatelessAndParsesTheAnswer(t *testing.T) {
	runner, argv := fakeRunner(t, accessCheckScript(`"{\"decision\":\"allow\",\"reason\":\"fixture grant covers the target\"}"`))
	result, err := runner.CheckAccess(t.Context(), accessCheckFixtureRequest("FIXTURE ACCESS ROLE\n"))
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	want := agent.AccessCheckResult{Decision: agent.AccessAllow, Reason: "fixture grant covers the target", Model: "gpt-test"}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	got := strings.Join(strings.Fields(string(mustRead(t, argv))), " ")
	for _, flag := range []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox read-only", "--model gpt-test"} {
		if !strings.Contains(got, flag) {
			t.Errorf("argv %q missing %q", got, flag)
		}
	}
	if strings.Contains(got, "--add-dir") || strings.Contains(got, "resume") {
		t.Errorf("access check argv carries run capability: %q", got)
	}
	prompt := string(mustRead(t, filepath.Join(filepath.Dir(argv), "stdin.dump")))
	if !strings.HasPrefix(prompt, "FIXTURE ACCESS ROLE\n") || !strings.Contains(prompt, "fixture grant") || !strings.Contains(prompt, "fixture comment") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestAccessCheckUsesDefaultRoleWhenBlank(t *testing.T) {
	runner, argv := fakeRunner(t, accessCheckScript(`"{\"decision\":\"deny\",\"reason\":\"outside the grant\"}"`))
	pkg, err := instructionpackage.Load(t.Context(), fstest.MapFS{
		"metadata.yaml":               {Data: []byte("schema: 1\nversion: fixture\n")},
		"catalogue.yaml":              {Data: []byte("- access-classifier\n")},
		"agents/access-classifier.md": {Data: []byte("FIXTURE DEFAULT ACCESS ROLE\n")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	var captured []string
	runner.instructions = advisorCaptureFunc(func(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
		captured = append(captured, role)
		return pkg.Capture(ctx, role)
	})
	result, err := runner.CheckAccess(t.Context(), accessCheckFixtureRequest(""))
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if result.Decision != agent.AccessDeny || result.Reason != "outside the grant" {
		t.Fatalf("result = %+v", result)
	}
	if len(captured) != 1 || captured[0] != "access-classifier" {
		t.Fatalf("captured roles = %v", captured)
	}
	if prompt := string(mustRead(t, filepath.Join(filepath.Dir(argv), "stdin.dump"))); !strings.HasPrefix(prompt, "FIXTURE DEFAULT ACCESS ROLE\n") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestAccessCheckRejectsBadAnswersAndFailedTurns(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"wrong decision", accessCheckScript(`"{\"decision\":\"maybe\",\"reason\":\"ok\"}"`), "invalid answer"},
		{"trailing prose", accessCheckScript(`"{\"decision\":\"allow\",\"reason\":\"ok\"} done"`), "invalid answer"},
		{"multi-line reason", accessCheckScript(`"{\"decision\":\"allow\",\"reason\":\"a\\nb\"}"`), "invalid answer"},
		{"no terminal turn", `printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"{\"decision\":\"allow\",\"reason\":\"ok\"}"}}'`, "no successful terminal result"},
		{"failed turn", `printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"{\"decision\":\"allow\",\"reason\":\"ok\"}"}}'
printf '%s\n' '{"type":"turn.failed","error":{"message":"boom"}}'`, "no successful terminal result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner, _ := fakeRunner(t, tc.body)
			result, err := runner.CheckAccess(t.Context(), accessCheckFixtureRequest("FIXTURE ACCESS ROLE\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if result != (agent.AccessCheckResult{}) {
				t.Fatalf("partial result on error: %+v", result)
			}
		})
	}
}

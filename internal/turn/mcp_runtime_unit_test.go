package turn

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

type mcpGateRunner struct {
	supported bool
	request   agent.Request
	result    *agent.RunResult
	err       error
	calls     int
}

func (r *mcpGateRunner) SupportsMCP() bool { return r.supported }
func (r *mcpGateRunner) Run(_ context.Context, req agent.Request) (*agent.RunResult, error) {
	r.calls++
	r.request = req
	return r.result, r.err
}

func TestRunMCPBound_DisabledAndInvalidRuntime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		runtime   *MCPRuntime
		supported bool
		reject    bool
	}{
		{"absent runtime/preserves provider request", nil, true, false},
		{"unsupported provider/still requires valid shared runtime", &MCPRuntime{}, false, true},
		{"enabled incomplete runtime/refuses before host launch", &MCPRuntime{}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hostErr := errors.New("fake host result")
			hostResult := &agent.RunResult{}
			runner := &mcpGateRunner{supported: tc.supported, result: hostResult, err: hostErr}
			req := agent.Request{Mode: "research", Permissions: agent.Permissions{Profile: "research", Payload: []string{"Read", "Bash"}}}
			got, err := runMCPBound(t.Context(), &preparation{runner: runner, mcpRuntime: tc.runtime}, req, nil)
			if tc.reject {
				if err == nil || errors.Is(err, hostErr) || got != nil || runner.calls != 0 {
					t.Fatalf("invalid runtime reached host: result=%v error=%v calls=%d", got, err, runner.calls)
				}
				return
			}
			if got != hostResult || !errors.Is(err, hostErr) || !reflect.DeepEqual(runner.request, req) {
				t.Fatalf("disabled setup changed request/result: request=%+v result=%v error=%v", runner.request, got, err)
			}
		})
	}
}

func TestBuildMCPLaunchSpec_UnavailableAuthority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		enabled bool
		guard   *AttemptGuard
		reject  bool
	}{
		{"disabled/no resources required", false, &AttemptGuard{}, false},
		{"absent authority/no registration", true, nil, false},
		{"enabled missing broker/refuses registration", true, &AttemptGuard{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec, err := BuildMCPLaunchSpec(t.Context(), tc.enabled, "invalid-path-must-not-be-used", nil, tc.guard)
			if spec != nil || (err != nil) != tc.reject {
				t.Fatalf("registration=%v error=%v, want nil registration and reject=%v", spec, err, tc.reject)
			}
		})
	}
}

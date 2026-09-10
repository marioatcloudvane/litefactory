package turn

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"testing"
)

func TestRequestModeHandlesEmptyAndExplicitMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Request
		want string
	}{
		{"zero request", Request{}, harness.InferMode},
		{"slash only", Request{Command: Command{Name: "/"}}, harness.InferMode},
		{"validate command", Request{Command: ParseCommand("/validate assess")}, "validate"},
		{"explicit takes precedence", Request{Mode: agent.ModeImplement, Command: ParseCommand("/validate assess")}, "implement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.mode(); got != tc.want {
				t.Fatalf("mode=%q want%q", got, tc.want)
			}
		})
	}
}

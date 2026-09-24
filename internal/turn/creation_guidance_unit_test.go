package turn

import "testing"

// A nil embedded forge fails if observation accidentally performs remote I/O.
type guidanceNoIOForge struct{ CreationForge }

func TestMCPRuntime_CreationPublishingAvailable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(**MCPRuntime, *mcpGateRunner)
		want   bool
	}{
		{"configured native capability/available", func(**MCPRuntime, *mcpGateRunner) {}, true},
		{"nil runtime/unavailable", func(r **MCPRuntime, _ *mcpGateRunner) { *r = nil }, false},
		{"disabled MCP/unavailable", func(r **MCPRuntime, _ *mcpGateRunner) { (*r).DisableMCP = true }, false},
		{"missing authorizer/unavailable", func(r **MCPRuntime, _ *mcpGateRunner) { (*r).CreationAuthorizer = nil }, false},
		{"disabled creation/unavailable", func(r **MCPRuntime, _ *mcpGateRunner) { (*r).CreationAuthorizer.enabled = false }, false},
		{"missing forge/unavailable", func(r **MCPRuntime, _ *mcpGateRunner) { (*r).Creation = nil }, false},
		{"unsupported native runner/unavailable", func(_ **MCPRuntime, r *mcpGateRunner) { r.supported = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runtime := &MCPRuntime{CreationAuthorizer: &CreationAuthorizer{enabled: true}, Creation: &guidanceNoIOForge{}}
			runner := &mcpGateRunner{supported: true}
			tc.change(&runtime, runner)

			got := runtime.CreationPublishingAvailable(runner)

			if got != tc.want {
				t.Fatalf("available=%v, want %v", got, tc.want)
			}
			if runner.calls != 0 {
				t.Fatal("capability observation started a run")
			}
		})
	}
}

package turn

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"strings"
	"testing"
)

func TestStackSetupOnlyAdmittedWriteImplementEngineers(t *testing.T) {
	for _, tt := range []struct {
		name, mode, workflow, role, access string
		allowed                            bool
	}{
		{"story worker", "implement", "implement", "story-worker", "write", true},
		{"neutral engineer", "implement", "implement", "implementation-engineer", "write", true},
		{"legacy engineer", "implement", "implement", "go-implementation-engineer", "write", true},
		{"validate", "validate", "validate", "implementation-engineer", "read", false},
		{"spike", "spike", "spike", "implementation-engineer", "write", false},
		{"bug with implement mode", "implement", "bug", "implementation-engineer", "write", false},
		{"read implement", "implement", "implement", "story-worker", "read", false},
		{"foreign role", "implement", "implement", "foreign", "write", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := agent.Request{Mode: tt.mode, Workflow: agent.Workflow(tt.workflow)}
			attrs := map[string]string{graphexec.AccessAttribute: tt.access, "lf_agent_markdown": tt.role}
			if got := stackEngineeringRequest(req, attrs); got != tt.allowed {
				t.Fatalf("eligibility=%v want%v", got, tt.allowed)
			}
			if !tt.allowed {
				p, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"setup":[{"name":"setup","argv":["tool"],"cwd":".","timeout_seconds":10}]}`), nil)
				if err != nil {
					t.Fatal(err)
				}
				req.StackProfile = &p
				results, err := runStackSetup(t.Context(), Options{}, req, attrs)
				if err != nil || len(results) != 0 {
					t.Fatal("excluded workflow attempted setup", err)
				}
			}
		})
	}
}
func TestStackGuidancePreservesEffectiveRecipeAndPinnedAuthority(t *testing.T) {
	p, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"checks":[{"name":"test","argv":["tool","test"],"cwd":".","timeout_seconds":10}],"environment":[{"name":"CI","value":"1"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stackEngineeringGuidance(agent.Request{StackProfile: &p})
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{p.EffectiveDigest, `"argv":["tool","test"]`, `"name":"CI","value":"1"`, "pinned Bug/Spike", "read-only Validate"} {
		if !strings.Contains(got, term) {
			t.Fatal("missing retained contract", term)
		}
	}
	p.EffectiveDigest = "tampered"
	if _, err = stackEngineeringGuidance(agent.Request{StackProfile: &p}); err == nil {
		t.Fatal("tampered profile rendered")
	}
}

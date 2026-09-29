package agent_test

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"testing"
)

func TestFixerPermissions_ScopedResultAuthority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		tools      []string
		missingMCP bool
		mode       agent.Mode
		profile    string
		wantErr    bool
	}{
		{name: "exact result surface", tools: []string{"communication_preflight", "communication_submit", "communication_receipt"}},
		{name: "missing receipt", tools: []string{"communication_preflight", "communication_submit"}, wantErr: true},
		{name: "publication forbidden", tools: []string{"communication_preflight", "communication_submit", "communication_receipt", "issues_comment"}, wantErr: true},
		{name: "checkpoint authority forbidden", tools: []string{"communication_preflight", "communication_submit", "communication_receipt", "evidence_submit"}, wantErr: true},
		{name: "missing service", missingMCP: true, wantErr: true},
		{name: "mode surrogate forbidden", tools: []string{"communication_preflight", "communication_submit", "communication_receipt"}, mode: agent.ModeImplement, wantErr: true},
		{name: "unknown profile forbidden", tools: []string{"communication_preflight", "communication_submit", "communication_receipt"}, profile: "implement", wantErr: true},
		// Story-003 AC-010: the two self-check tools are permitted only for
		// the bug-fixer profile.
		{name: "bug-fixer self-check tools permitted", profile: "bug-fixer", tools: []string{"communication_preflight", "communication_submit", "communication_receipt", "bug_self_check_start", "bug_self_check_status"}},
		{name: "bug-fixer self-check start alone permitted", profile: "bug-fixer", tools: []string{"communication_preflight", "communication_submit", "communication_receipt", "bug_self_check_start"}},
		{name: "bug-analyst self-check start forbidden", profile: "bug-analyst", tools: []string{"communication_preflight", "communication_submit", "communication_receipt", "bug_self_check_start"}, wantErr: true},
		{name: "bug-analyst self-check status forbidden", profile: "bug-analyst", tools: []string{"communication_preflight", "communication_submit", "communication_receipt", "bug_self_check_status"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := agent.Request{Workflow: agent.WorkflowBug, Mode: string(tt.mode), Invocation: agent.InvocationResearchNode, Permissions: agent.Permissions{Profile: "bug-fixer"}}
			if tt.profile != "" {
				req.Permissions.Profile = tt.profile
			}
			if !tt.missingMCP {
				launch, err := agent.NewMCPLaunchSpec("/usr/local/bin/litefactory", "/tmp/descriptor.json", "attempt-1", tt.tools...)
				if err != nil {
					t.Fatal(err)
				}
				req.MCPLaunch = launch
			}
			err := req.ValidateBugAnalyst()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validation error=%v want error=%v", err, tt.wantErr)
			}
		})
	}
}

package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func guidanceSpec(t *testing.T, names ...string) *agent.MCPLaunchSpec {
	t.Helper()
	spec, err := agent.NewMCPLaunchSpec("/factory/bin/litefactory", "/factory/private/descriptor.json", "isolated-attempt", names...)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func guidanceSession(stage string) *SubmissionSession {
	return &SubmissionSession{scope: agent.ContributionScope{ContractVersion: 2, WorkflowVersion: 2, Stage: stage, Stories: map[string]agent.ContributionStory{
		"US-004": {Revision: strings.Repeat("a", 64), SnapshotID: "retained-snapshot", ImplementationDigest: strings.Repeat("b", 64), SnapshotRetained: true, Criteria: []string{"AC-006", "AC-007"}},
	}}}
}

func TestMCPGuidance_RegistrationRequired(t *testing.T) {
	t.Parallel()
	got, err := mcpGuidance(t.Context(), nil, nil, nil)
	if err != nil || got != "" {
		t.Fatalf("absent registration: %q, %v", got, err)
	}
	if _, err := mcpGuidance(t.Context(), guidanceSpec(t, "activity_get"), nil, nil); err == nil {
		t.Fatal("live guidance accepted missing service")
	}
}

// The operation list names exactly the installed tools, and an example is
// rendered only when its whole route is installed. What the tools are for is
// in the result-protocol skill; nothing here pins that prose.
func TestMCPGuidance_InstalledOperations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		tools           []string
		present, absent []string
	}{
		{"lookup list", []string{"issues_list"}, []string{"- issues_list\n"}, []string{"- issues_get\n", "- issues_comment\n", "- communication_preflight\n", "```json"}},
		{"authorized comments", []string{"issues_comment", "receipts_get"}, []string{"- issues_comment\n", "- receipts_get\n"}, []string{"- issues_list\n"}},
		{"partial application", []string{"communication_preflight", "communication_submit"}, []string{"- communication_preflight\n"}, []string{"```json"}},
		{"partial checkpoint", []string{"feedback_preflight", "evidence_submit"}, []string{"- evidence_submit\n"}, []string{"```json"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := mcpGuidance(t.Context(), guidanceSpec(t, tt.tools...), &AgentResultService{binding: store.ResultBinding{Mode: "research"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.present {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q", want)
				}
			}
			for _, avoid := range tt.absent {
				if strings.Contains(got, avoid) {
					t.Errorf("advertises unsupported %q", avoid)
				}
			}
			if strings.Contains(got, "/factory/") || strings.Contains(got, "isolated-attempt") {
				t.Error("guidance exposes registration identity")
			}
		})
	}
}

func TestMCPGuidance_CheckpointContract(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ mode, stage string }{{"implement", "engineering"}, {"validate", "validate"}} {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			session := guidanceSession(tt.stage)
			got, err := mcpGuidance(t.Context(), guidanceSpec(t, "communication_preflight", "communication_submit", "communication_receipt", "feedback_preflight", "evidence_submit", "receipts_get"), &AgentResultService{binding: store.ResultBinding{Mode: tt.mode}}, session)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(got, "```json\n") != 1 || strings.Contains(got, "Application P example") {
				t.Fatal("guidance must supply one relevant checkpoint contract")
			}
			_, tail, ok := strings.Cut(got, "```json\n")
			if !ok {
				t.Fatal("missing JSON")
			}
			payload, _, ok := strings.Cut(tail, "\n```")
			if !ok {
				t.Fatal("unterminated JSON")
			}
			contribution, d := agent.ParseContribution([]byte(payload), session.scope)
			if !d.Valid || !d.Complete {
				t.Fatalf("example does not satisfy supplied contract: %+v", d)
			}
			if contribution.Story != "US-004" {
				t.Errorf("invented story %q", contribution.Story)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(payload), &fields); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"task", "parent", "attempt", "revision", "version", "counter"} {
				if _, ok := fields[name]; ok {
					t.Errorf("factory field %q", name)
				}
			}
			if tt.stage == "engineering" {
				if contribution.Outcome != "blocked" || contribution.Engineering != nil {
					t.Error("illustrative example falsely claims engineering execution")
				}
			} else {
				if contribution.Assessment == nil || contribution.Assessment.SnapshotID != "retained-snapshot" {
					t.Fatal("missing trusted retained snapshot")
				}
				if len(contribution.Assessment.Criteria) != 2 || len(contribution.Assessment.Reviews) != 3 {
					t.Fatal("incomplete supplied assessment coverage")
				}
				for _, finding := range append(contribution.Assessment.Criteria, contribution.Assessment.Reviews...) {
					if finding.Status != "unverified" {
						t.Error("example invents satisfied evidence")
					}
				}
			}
		})
	}
}

func TestMCPGuidance_CheckpointRequiresScope(t *testing.T) {
	t.Parallel()
	for _, session := range []*SubmissionSession{nil, {}} {
		if _, err := mcpCheckpointExample(session); err == nil {
			t.Error("checkpoint accepted missing frozen scope")
		}
	}
}

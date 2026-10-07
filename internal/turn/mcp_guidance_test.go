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

// Each card is rendered exactly when its tools are installed; the tool call it
// shows is the observable part, its prose is not pinned.
func TestMCPGuidance_InstalledCards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		tools           []string
		present, absent []string
	}{
		{"lookup list", []string{"issues_list"}, []string{"issues_list({"}, []string{"issues_get({", "issues_comment(", "issues_create_batch(", "activity_update(", "communication_preflight(", "evidence_submit("}},
		{"lookup get", []string{"issues_get"}, []string{"issues_get({"}, []string{"issues_list({"}},
		{"comment without receipt", []string{"issues_comment"}, nil, []string{"issues_comment("}},
		{"authorized comments", []string{"issues_comment", "receipts_get"}, []string{"issues_comment({", "receipts_get"}, []string{"issues_create_batch("}},
		{"activity update", []string{"activity_update"}, []string{"activity_update({"}, []string{"activity_get({"}},
		{"activity read", []string{"activity_get"}, []string{"activity_get({"}, []string{"activity_update({"}},
		{"creation missing preflight", []string{"issues_create_batch", "receipts_get"}, nil, []string{"issues_create_batch("}},
		{"partial application", []string{"communication_preflight", "communication_submit"}, nil, []string{"communication_preflight(", "```json"}},
		{"partial checkpoint", []string{"feedback_preflight", "evidence_submit"}, nil, []string{"evidence_submit(", "```json"}},
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

// Every supported mode adds its own restriction to the shared guidance.
func TestMCPGuidance_ModeRestrictions(t *testing.T) {
	t.Parallel()
	render := func(mode string) string {
		got, err := mcpGuidance(t.Context(), guidanceSpec(t, "activity_get"), &AgentResultService{binding: store.ResultBinding{Mode: mode}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	base := render("")
	for _, mode := range []string{"plan", "validate", "implement", "research", "spike"} {
		if got := render(mode); !strings.HasPrefix(got, base) || len(got) == len(base) {
			t.Errorf("%s guidance carries no mode restriction", mode)
		}
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
			if strings.Count(got, "```json\n") != 1 || strings.Contains(got, "Application P example:") {
				t.Fatal("guidance must supply one relevant checkpoint contract")
			}
			for _, want := range []string{"Application results:", "communication_preflight(P) → communication_submit(P) → communication_receipt", `feedback_preflight({"kind":"checkpoint","input":C}) → evidence_submit(C) → receipts_get`, "separately from application result P", "partial live checkpoints never excuse omissions"} {
				if !strings.Contains(got, want) {
					t.Errorf("missing operation separation %q", want)
				}
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

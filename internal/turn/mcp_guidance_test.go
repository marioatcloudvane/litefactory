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

func TestMCPGuidance_InstalledCards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		tools           []string
		present, absent []string
	}{
		{"lookup list", []string{"issues_list"}, []string{"issues_list({", "not full bodies or comments"}, []string{"issues_get({", "Comments:", "Creation:", "Progress:", "Application results:", "Checkpoints:"}},
		{"lookup get", []string{"issues_get"}, []string{"issues_get({", "repository-local summary"}, []string{"issues_list({"}},
		{"comment without receipt", []string{"issues_comment"}, nil, []string{"Comments:"}},
		{"authorized comments", []string{"issues_comment", "receipts_get"}, []string{"Comments:", "only to authorized targets", "inspect receipts_get"}, []string{"Creation:"}},
		{"activity update", []string{"activity_update"}, []string{"activity_update({", "advisory activity, not completion", "increasing sequence"}, []string{"activity_get({"}},
		{"activity read", []string{"activity_get"}, []string{"activity_get({}", "unavailable after restart"}, []string{"activity_update({"}},
		{"creation missing preflight", []string{"issues_create_batch", "receipts_get"}, nil, []string{"Creation:"}},
		{"separately granted creation", []string{"issues_create_batch", "receipts_get", "feedback_preflight"}, []string{"Creation:", "separate live factory grant", "Tool visibility alone is not creation approval"}, []string{"Checkpoints:"}},
		{"partial application", []string{"communication_preflight", "communication_submit"}, nil, []string{"Application results:", "Application P example:"}},
		{"partial checkpoint", []string{"feedback_preflight", "evidence_submit"}, nil, []string{"Checkpoints:", "Checkpoint C example:"}},
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
			for _, want := range []string{"supplement your existing permitted tools", "Ordinary runs need not", "required final response and artifacts", "correct every reported diagnostic", "receipt before exit", "acknowledgment is not acceptance or publication", "Pending, failed and uncertain", "look up the known request before retrying", "do not mutate input solely"} {
				if !strings.Contains(got, want) {
					t.Errorf("missing shared obligation %q", want)
				}
			}
			if strings.Contains(got, "/factory/") || strings.Contains(got, "isolated-attempt") {
				t.Error("guidance exposes registration identity")
			}
		})
	}
}

func TestMCPGuidance_ModeRestrictions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ mode, want string }{
		{"plan", "Keep the supplied typed Plan helper publication path; communication tools cannot publish a completed Plan"},
		{"validate", "no shell, edits, fresh checks or delegation escapes. Keep the required final findings"},
		{"implement", "Preserve acceptance for Validate and report only checks actually executed"},
		{"research", "Recommendations remain advisory"}, {"spike", "without selecting a new mode"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			got, err := mcpGuidance(t.Context(), guidanceSpec(t, "activity_get"), &AgentResultService{binding: store.ResultBinding{Mode: tt.mode}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("missing mode restriction %q", tt.want)
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

func TestMCPGuidance_RenderMetrics(t *testing.T) {
	t.Logf("shared words=%d bytes=%d", len(strings.Fields(mcpSharedGuidance)), len(mcpSharedGuidance))
	tools := []string{"communication_preflight", "communication_submit", "communication_receipt", "feedback_preflight", "evidence_submit", "receipts_get", "issues_list", "issues_get", "issues_comment", "activity_update", "activity_get", "issues_create_batch"}
	for _, mode := range []string{"implement", "validate"} {
		stage := "engineering"
		if mode == "validate" {
			stage = "validate"
		}
		got, err := mcpGuidance(t.Context(), guidanceSpec(t, tools...), &AgentResultService{binding: store.ResultBinding{Mode: mode}}, guidanceSession(stage))
		if err != nil {
			t.Fatal(err)
		}
		before, tail, _ := strings.Cut(got, "```json\n")
		payload, after, _ := strings.Cut(tail, "\n```")
		without := before + after
		t.Logf("%s full granted card catalog: excluding JSON contract words=%d bytes=%d; including contract words=%d bytes=%d; contract bytes=%d", mode, len(strings.Fields(without)), len(without), len(strings.Fields(got)), len(got), len(payload))
	}
	t.Log("Actual provider-rendered token measurements including tool schemas/examples: unavailable; no host acquisition or tokenizer estimate. Word/byte counts do not establish billed tokens or savings.")
}

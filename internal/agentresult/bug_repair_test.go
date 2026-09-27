package agentresult_test

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"strings"
	"testing"
)

func fixerResultFixture() (ar.Result, ar.Context) {
	ctx := ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Workflow: agent.WorkflowBug, Bug: &ar.BugBinding{Role: "bug-fixer", Invocation: "fix-1", BaseRevision: 2, ApprovedRevision: 2, ApprovedDigest: "analysis-digest", ApprovalReview: "review-1", Reservation: "repair-1", SourceCommit: "source", SourceTree: "tree", CriteriaJSON: `["AC-001"]`}}
	r := ar.Result{Outcome: "completed", Comment: "Authored repair and regression coverage; checks remain pending.", Payload: ar.Payload{Bug: &ar.Bug{Response: "done", Invocation: "fix-1", BaseRevision: 2, Repair: &bugrepair.Evidence{Reservation: "repair-1", AnalysisDigest: "analysis-digest", SourceCommit: "source", SourceTree: "tree", Summary: "Guard empty input", Coverage: []bugrepair.Coverage{{Criterion: "AC-001", Changes: []string{"search.go"}, Tests: []string{"search_test.go"}}}}}}}
	return r, ctx
}
func TestFixerResult_ResponseContracts(t *testing.T) {
	t.Parallel()
	for _, response := range []string{"done", "reshape", "blocked"} {
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			r, ctx := fixerResultFixture()
			r.Payload.Bug.Response = response
			if response != "done" {
				r.Reason = "Approved remedy cannot be completed"
				r.Outcome = "blocked"
				if response == "reshape" {
					r.Outcome = "incomplete"
				}
				r.Payload.Bug.Repair.Coverage = []bugrepair.Coverage{}
			}
			assertValid(t, r, ctx)
			data, err := json.Marshal(ar.ResearchNode{Version: 1, Response: response, Result: r})
			if err != nil {
				t.Fatal(err)
			}
			_, d := ar.DecodeBugNode(data, ctx)
			if !d.Valid || !d.Complete {
				t.Fatalf("node rejected: %+v", d)
			}
		})
	}
}
func TestFixerResult_RefusesAuthorityAndBindingChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*ar.Result, *ar.Context)
	}{
		{"approval response", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "approved" }},
		{"analyst question response", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "ask" }},
		{"missing approval", func(_ *ar.Result, c *ar.Context) { c.Bug.ApprovedRevision = 0 }},
		{"missing review", func(_ *ar.Result, c *ar.Context) { c.Bug.ApprovalReview = "" }},
		{"wrong role", func(_ *ar.Result, c *ar.Context) { c.Bug.Role = "bug-analyst" }},
		{"stale invocation", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Invocation = "other" }},
		{"stale revision", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.BaseRevision++ }},
		{"different reservation", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair.Reservation = "other" }},
		{"different approval", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair.AnalysisDigest = "other" }},
		{"different source", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair.SourceCommit = "other" }},
		{"different tree", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair.SourceTree = "other" }},
		{"payload grants approval", func(r *ar.Result, _ *ar.Context) {
			r.Payload.Bug.Approval = &ar.BugApprovalReference{Revision: 2, Digest: "analysis-digest", Review: "review-1"}
		}},
		{"missing evidence", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair = nil }},
		{"missing criterion context", func(_ *ar.Result, c *ar.Context) { c.Bug.CriteriaJSON = "[]" }},
		{"done with gap", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair.Coverage[0].Gap = "No regression" }},
		{"done with incomplete outcome", func(r *ar.Result, _ *ar.Context) { r.Outcome = "incomplete"; r.Reason = "not done" }},
		{"reshape without reason", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "reshape"; r.Outcome = "incomplete" }},
		{"blocked without reason", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "blocked"; r.Outcome = "blocked" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, ctx := fixerResultFixture()
			tt.change(&r, &ctx)
			d := ar.ValidateResult(r, ctx)
			if d.Valid {
				t.Fatal("accepted unauthorized or inconsistent repair")
			}
		})
	}
}

func TestFixerResult_RejectsAuthoredExecutionAuthority(t *testing.T) {
	t.Parallel()
	for _, field := range []string{`"repair_used":0`, `"approved_revision":2`, `"recipe":"echo pass"`, `"delivery_approved":true`, `"checks_passed":true`} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			r, ctx := fixerResultFixture()
			raw := strings.Replace(string(authored(t, r)), `"bug":{`, `"bug":{`+field+`,`, 1)
			_, d := ar.Create([]byte(raw), ctx)
			if d.Valid {
				t.Fatal("authored execution authority accepted")
			}
		})
	}
}

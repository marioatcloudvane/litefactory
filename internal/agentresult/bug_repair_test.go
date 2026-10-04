package agentresult_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
)

func fixerResultFixture() (ar.Result, ar.Context) {
	ctx := ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Workflow: agent.WorkflowBug, Bug: &ar.BugBinding{Role: "bug-fixer", Invocation: "fix-1", BaseRevision: 2, ApprovedRevision: 2}}
	r := ar.Result{Outcome: "completed", Comment: "Fixed and checked.", Payload: ar.Payload{Bug: &ar.Bug{Response: "succeeded", Repair: &bugrepair.Evidence{Summary: "Guard empty input", Checks: []bugrepair.Check{{Command: "go test ./...", Outcome: "passed"}}}}}}
	return r, ctx
}

// A fixer reports a response, its outcome, a reason when it did not succeed
// and a summary. It echoes no digests, reservations or revisions.
func TestFixerResult_ResponseContracts(t *testing.T) {
	t.Parallel()
	for _, response := range []string{"succeeded", "reshape", "blocked"} {
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			r, ctx := fixerResultFixture()
			r.Payload.Bug.Response = response
			if response != "succeeded" {
				r.Reason = "Approved remedy cannot be completed"
				r.Outcome = "blocked"
				if response == "reshape" {
					r.Outcome = "incomplete"
				}
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

func TestFixerResult_RefusesInconsistentReports(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*ar.Result, *ar.Context)
	}{
		{"approval response", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "approved" }},
		{"analyst question response", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "ask" }},
		{"wrong role", func(_ *ar.Result, c *ar.Context) { c.Bug.Role = "bug-analyst" }},
		{"missing repair", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair = nil }},
		{"blank summary", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Repair.Summary = " " }},
		{"succeeded with incomplete outcome", func(r *ar.Result, _ *ar.Context) { r.Outcome = "incomplete"; r.Reason = "not done" }},
		{"reshape without reason", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "reshape"; r.Outcome = "incomplete" }},
		{"blocked without reason", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "blocked"; r.Outcome = "blocked" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, ctx := fixerResultFixture()
			tt.change(&r, &ctx)
			if d := ar.ValidateResult(r, ctx); d.Valid {
				t.Fatal("accepted inconsistent repair report")
			}
		})
	}
}

func TestFixerResult_RejectsAuthoredExecutionAuthority(t *testing.T) {
	t.Parallel()
	for _, field := range []string{`"repair_used":0`, `"approved_revision":2`, `"recipe":"echo pass"`, `"delivery_approved":true`, `"approval":{}`} {
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

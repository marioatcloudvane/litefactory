package agentresult_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	ba "github.com/cloudvane/litefactory/internal/buganalysis"
)

func bugResultFixture(response string) (ar.Result, ar.Context) {
	ctx := ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Workflow: agent.WorkflowBug, QuestionsAllowed: true, Bug: &ar.BugBinding{Role: "bug-analyst", Invocation: "invocation-1", BaseRevision: 2}}
	p := &ar.Bug{Response: response, Invocation: "invocation-1", BaseRevision: 2}
	r := ar.Result{Outcome: "completed", Comment: "Retained analysis for owner review.", Payload: ar.Payload{Bug: p}}
	switch response {
	case "done":
		p.Analysis = &ba.BugAnalysis{Expected: "Empty queries return no matches.", Actual: "Empty queries panic.", Reproduction: ba.BugReproduction{Steps: []string{"Search an empty query."}, Environment: "Go fixture", Observation: "owner_reported"}, Cause: "Unchecked byte access.", Evidence: []string{"search.go:12"}, Remedy: "Guard empty query before access.", Exclusions: []string{"Do not alter nonempty matching."}, Criteria: []ba.BugCriterion{{ID: "AC-001", Requirement: "No panic on empty query.", Measurement: "Regression test asserts zero matches."}}}
	case "ask":
		r.Outcome, r.Reason = "incomplete", "Need the failing input."
		r.Questions = []ar.Question{{ID: "Q-001", Audience: "technical", Text: "Which input failed?"}}
		p.Analysis = &ba.BugAnalysis{Actual: "Reported panic", Reproduction: ba.BugReproduction{Steps: []string{}}, Evidence: []string{}, Exclusions: []string{}, Criteria: []ba.BugCriterion{}, Questions: []ba.BugQuestion{{ID: "Q-001", Text: "Which input failed?", Material: true}}}
	case "blocked":
		r.Outcome, r.Reason = "blocked", "The reproduction requires unavailable owner evidence."
		p.Evidence = []string{"owner-comment-123"}
	}
	return r, ctx
}

func TestBugResult_ValidResponses(t *testing.T) {
	t.Parallel()
	for _, response := range []string{"ask", "done", "blocked"} {
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			r, ctx := bugResultFixture(response)
			assertValid(t, r, ctx)
			node := ar.ResearchNode{Version: 1, Response: response, Result: r}
			data, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			got, d := ar.DecodeBugNode(data, ctx)
			if !d.Valid || !d.Complete || !reflect.DeepEqual(got, node) {
				t.Fatalf("node=%+v diagnostics=%+v", got, d)
			}
		})
	}
}

func TestBugResult_ResponseGuards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, response string
		change         func(*ar.Result, *ar.Context)
	}{
		{"ask requires incomplete", "ask", func(r *ar.Result, _ *ar.Context) { r.Outcome = "completed"; r.Reason = "" }},
		{"ask requires reason", "ask", func(r *ar.Result, _ *ar.Context) { r.Reason = "" }},
		{"ask requires snapshot", "ask", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Analysis = nil }},
		{"ask requires display batch", "ask", func(r *ar.Result, _ *ar.Context) { r.Questions = nil }},
		{"ask requires retained batch", "ask", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Analysis.Questions = nil }},
		{"ask display text mismatch", "ask", func(r *ar.Result, _ *ar.Context) { r.Questions[0].Text = "A different question" }},
		{"ask display ID mismatch", "ask", func(r *ar.Result, _ *ar.Context) { r.Questions[0].ID = "Q-002" }},
		{"done requires analysis", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Analysis = nil }},
		{"done requires evidenced cause", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Analysis.Evidence = nil }},
		{"done requires measurable criteria", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Analysis.Criteria[0].Measurement = "" }},
		{"done cannot ask", "done", func(r *ar.Result, _ *ar.Context) {
			r.Questions = []ar.Question{{ID: "Q-1", Audience: "technical", Text: "More scope?"}}
		}},
		{"analyst cannot approve", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "approved" }},
		{"blocked requires obstacle", "blocked", func(r *ar.Result, _ *ar.Context) { r.Reason = " " }},
		{"blocked requires evidence", "blocked", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Evidence = nil }},
		{"blocked requires nonblank evidence", "blocked", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Evidence = []string{" "} }},
		{"unknown response", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "merge" }},
		{"analyst cannot reshape", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Bug.Response = "reshape" }},
		{"no mode recommendation", "done", func(r *ar.Result, _ *ar.Context) {
			r.Recommendation = &ar.Recommendation{Text: "Implement now", Action: "action.moveto.implementmode"}
		}},
		{"multiple payload branches", "done", func(r *ar.Result, _ *ar.Context) { r.Payload.Research = &ar.Research{Request: "Investigate"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, c := bugResultFixture(tt.response)
			tt.change(&r, &c)
			_, d := ar.Create(authored(t, r), c)
			if d.Valid {
				t.Fatalf("invalid response accepted: %+v", r)
			}
			if len(d.Diagnostics) == 0 {
				t.Fatalf("refusal lacks diagnostics: %+v", d)
			}
		})
	}
}

func TestBugResult_BindingGuards(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(*ar.Context)
	}{
		{"missing binding", func(c *ar.Context) { c.Bug = nil }},
		{"wrong role", func(c *ar.Context) { c.Bug.Role = "bug-fixer" }},
		{"missing invocation", func(c *ar.Context) { c.Bug.Invocation = "" }},
		{"wrong workflow", func(c *ar.Context) { c.Workflow = agent.WorkflowResearch }},
		{"missing workflow", func(c *ar.Context) { c.Workflow = "" }},
		{"surrogate mode", func(c *ar.Context) { c.Mode = "research" }},
		{"bug is not a mode", func(c *ar.Context) { c.Mode = "bug" }},
		{"unsupported schema", func(c *ar.Context) { c.SchemaVersion = 2 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, c := bugResultFixture("done")
			tt.change(&c)
			_, d := ar.Create(authored(t, r), c)
			if d.Valid {
				t.Fatal("mismatched binding accepted")
			}
		})
	}
}

func TestBugResult_StrictInput(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		edit func(string) string
	}{
		{"duplicate response", func(s string) string {
			return strings.Replace(s, `"response":"done"`, `"response":"ask","response":"done"`, 1)
		}},
		{"unknown field", func(s string) string { return strings.Replace(s, `"bug":{`, `"bug":{"mystery":true,`, 1) }},
		{"authored counter", func(s string) string { return strings.Replace(s, `"bug":{`, `"bug":{"analysis_used":0,`, 1) }},
		{"authored approval authority", func(s string) string { return strings.Replace(s, `"bug":{`, `"bug":{"approved_revision":2,`, 1) }},
		{"authored recipe", func(s string) string { return strings.Replace(s, `"bug":{`, `"bug":{"recipe":"echo pass",`, 1) }},
		{"authored workflow", func(s string) string { return strings.Replace(s, `"result":{`, `"result":{"workflow":"bug",`, 1) }},
		{"wrong scalar type", func(s string) string { return strings.Replace(s, `"base_revision":2`, `"base_revision":"2"`, 1) }},
		{"truncated", func(s string) string { return s[:len(s)-1] }},
		{"trailing document", func(s string) string { return s + `{}` }},
		{"oversized", func(string) string { return strings.Repeat(" ", ar.MaxBytes+1) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, c := bugResultFixture("done")
			_, d := ar.Create([]byte(tt.edit(string(authored(t, r)))), c)
			if d.Valid {
				t.Fatal("malformed input accepted")
			}
		})
	}
}

func TestBugResult_StrictNode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		edit func(string) string
	}{
		{"unsupported version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":2`, 1) }},
		{"missing version", func(s string) string { return strings.Replace(s, `"version":1,`, "", 1) }},
		{"duplicate version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"version":1`, 1) }},
		{"unknown field", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"extra":true`, 1) }},
		{"response mismatch", func(s string) string { return strings.Replace(s, `"response":"done"`, `"response":"approved"`, 1) }},
		{"prose only", func(string) string { return "Analysis is done and approved." }},
		{"prose wrapped JSON", func(s string) string { return "Here is the result: " + s }},
		{"oversized", func(string) string { return strings.Repeat(" ", ar.MaxBytes+1) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, c := bugResultFixture("done")
			data, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: r})
			if err != nil {
				t.Fatal(err)
			}
			_, d := ar.DecodeBugNode([]byte(tt.edit(string(data))), c)
			if d.Valid {
				t.Fatal("invalid node accepted")
			}
		})
	}
}

func TestBugResult_MetadataProjectionAndUpdate(t *testing.T) {
	t.Parallel()
	for _, response := range []string{"ask", "done", "blocked"} {
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			r, c := bugResultFixture(response)
			meta := ar.Metadata{SchemaVersion: 1, Workflow: agent.WorkflowBug, Task: "task-1", RunID: 1, AttemptID: "attempt-1", ResultID: "result-1", ResultRevision: 1}
			snapshot, d := ar.Assemble(r, meta, c)
			if !d.Valid {
				t.Fatalf("assemble: %+v", d)
			}
			target := ar.Target{Field: "payload.bug"}
			projection, d := ar.Project(snapshot, []ar.Target{target}, ar.EditTargets("bug"), c)
			if !d.Valid || projection.ResultID != "result-1" || projection.Revision != 1 || len(projection.Selections) != 1 {
				t.Fatalf("projection=%+v diagnostics=%+v", projection, d)
			}
			var p ar.Bug
			if err := json.Unmarshal(projection.Selections[0].Value, &p); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(&p, r.Payload.Bug) {
				t.Fatal("projection lost Bug content")
			}
			before := authored(t, snapshot.Result)
			got, ud := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "comment", "", `"Updated explanation."`)}}, c, ar.EditTargets("bug"))
			if !ud.Valid {
				t.Fatalf("update: %+v", ud)
			}
			if got.Snapshot.Meta.Workflow != agent.WorkflowBug || got.Snapshot.Meta.Mode != "" || got.Snapshot.Meta.ResultRevision != 2 || got.Snapshot.Result.Comment != "Updated explanation." {
				t.Fatalf("lost identity or update: %+v", got.Snapshot)
			}
			if !reflect.DeepEqual(got.Snapshot.Result.Payload, r.Payload) || !reflect.DeepEqual(before, authored(t, snapshot.Result)) {
				t.Fatal("update mutated baseline or untouched payload")
			}
		})
	}
}

func TestBugResult_RefusesMetadataAndUpdateAuthorityChanges(t *testing.T) {
	t.Parallel()
	r, c := bugResultFixture("done")
	snapshot := ar.Snapshot{Meta: ar.Metadata{SchemaVersion: 1, Workflow: agent.WorkflowBug, Task: "task-1", RunID: 1, AttemptID: "attempt-1", ResultID: "result-1", ResultRevision: 1}, Result: r}
	for _, tt := range []struct {
		name string
		op   ar.Operation
	}{
		{"workflow", operation("set", "meta.workflow", "", `"research"`)},
		{"mode", operation("set", "meta.mode", "", `"research"`)},
		{"counter", operation("set", "payload.bug.analysis_used", "", `0`)},
		{"self approval", operation("set", "payload.bug", "", `{"response":"approved","invocation":"invocation-1","base_revision":2,"approval":{"revision":2,"digest":"a","review":"review-1"}}`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{tt.op}}, c, ar.EditTargets("bug"))
			if d.Valid {
				t.Fatal("authority-changing update accepted")
			}
		})
	}
	t.Run("wrong snapshot workflow", func(t *testing.T) {
		s := snapshot
		s.Meta.Workflow = agent.WorkflowPlan
		_, d := ar.Project(s, []ar.Target{{Field: "payload.bug"}}, ar.EditTargets("bug"), c)
		if d.Valid {
			t.Fatal("wrong workflow projection accepted")
		}
	})
	t.Run("stale update", func(t *testing.T) {
		_, d := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 0, Operations: []ar.Operation{operation("set", "comment", "", `"Changed"`)}}, c, ar.EditTargets("bug"))
		if d.Valid {
			t.Fatal("stale update accepted")
		}
	})
}

func TestBugResult_UpdateProposalRetainsInvocation(t *testing.T) {
	t.Parallel()
	r, c := bugResultFixture("done")
	snapshot := ar.Snapshot{Meta: ar.Metadata{SchemaVersion: 1, Workflow: agent.WorkflowBug, Task: "task-1", RunID: 1, AttemptID: "attempt-1", ResultID: "result-1", ResultRevision: 1}, Result: r}
	changed, _ := bugResultFixture("done")
	changed.Payload.Bug.Analysis.Remedy = "Guard empty query at the public entry point."
	data, err := json.Marshal(changed.Payload.Bug)
	if err != nil {
		t.Fatal(err)
	}
	got, d := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "payload.bug", "", string(data))}}, c, ar.EditTargets("bug"))
	if !d.Valid {
		t.Fatalf("valid proposal update refused: %+v", d)
	}
	if !reflect.DeepEqual(got.Snapshot.Result.Payload.Bug, changed.Payload.Bug) || got.Snapshot.Meta.ResultRevision != 2 || got.Snapshot.Meta.Workflow != agent.WorkflowBug || got.Snapshot.Meta.Mode != "" {
		t.Fatalf("updated proposal or identity lost: %+v", got.Snapshot)
	}
	if snapshot.Result.Payload.Bug.Analysis.Remedy == changed.Payload.Bug.Analysis.Remedy {
		t.Fatal("update changed original proposal")
	}
}

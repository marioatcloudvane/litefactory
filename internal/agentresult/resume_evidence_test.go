package agentresult_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func TestUpdate_CorrectionBaselineIsScopedAndReusable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		revision int64
		value    string
	}{
		{"stale revision", 6, `{"title":"Corrected title"}`},
		{"malformed field", 7, `{"title":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, c := updateSnapshot("plan", "incomplete")
			s.Meta.ResultRevision = 7
			s.Result.Comment = "sibling-private-comment"
			s.Result.Payload.Plan.Stories = append(s.Result.Payload.Plan.Stories, ar.PlanStory{Key: "US-002", Title: "sibling-private-title", UserStory: ar.UserStory{Want: "independent work"}})
			target := ar.Target{Field: "payload.plan.stories", ID: "US-001"}
			grants := []ar.Target{target}
			before := authored(t, s.Result)
			got, d := ar.ApplyUpdate(s, nil, ar.UpdateInput{ExpectedRevision: tc.revision, Operations: []ar.Operation{operation("update_item", target.Field, target.ID, tc.value)}}, c, grants)
			if d.Valid || !reflect.DeepEqual(got, ar.PreparedUpdate{}) {
				t.Fatalf("missing rejected correction baseline: %+v", d)
			}
			projection, projectionDiagnostics := ar.Project(s, grants, grants, c)
			if !projectionDiagnostics.Valid {
				t.Fatalf("authorized read route failed: %+v", projectionDiagnostics)
			}
			if d.CurrentRevision != 7 || projection.Revision != 7 || projection.ResultID != s.Meta.ResultID || projection.CompleteSnapshot || len(projection.Selections) != 1 || projection.Selections[0].Target != target {
				t.Fatalf("wrong scoped baseline: %+v", d)
			}
			b, err := json.Marshal(struct {
				Diagnostic ar.UpdateDiagnostics
				Baseline   ar.Projection
			}{d, projection})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "sibling-private") || strings.Contains(string(b), "US-002") {
				t.Fatalf("correction exposes sibling: %s", b)
			}
			if len(d.Diagnostics.Diagnostics) == 0 || d.Diagnostics.Diagnostics[0].Path == "" || d.Diagnostics.Diagnostics[0].Message == "" {
				t.Fatal("missing actionable field diagnostic")
			}
			fixed, diag := ar.ApplyUpdate(s, nil, ar.UpdateInput{ExpectedRevision: projection.Revision, Operations: []ar.Operation{operation("update_item", target.Field, target.ID, `{"title":"Corrected title"}`)}}, c, grants)
			if !diag.Valid || fixed.Snapshot.Meta.ResultRevision != 8 || fixed.Snapshot.Result.Payload.Plan.Stories[0].Title != "Corrected title" || fixed.Snapshot.Result.Payload.Plan.Stories[1].Title != "sibling-private-title" {
				t.Fatalf("authorized correction failed: %+v", diag)
			}
			projection.Selections[0].Value[0] = '!'
			if !reflect.DeepEqual(before, authored(t, s.Result)) {
				t.Fatal("correction or projection mutated retained baseline")
			}
		})
	}
}

func TestUpdate_UntouchedCompletionEvidenceRevalidated(t *testing.T) {
	for _, fault := range []string{"valid", "unretained snapshot", "missing check", "wrong revision", "failed check", "missing resolver", "stale aggregate"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			s, c := updateSnapshot("implement", "completed")
			s.Result.Payload.Implement.AggregateChecks = &agent.EngineeringContribution{SnapshotID: "aggregate", CheckIDs: []string{"aggregate-check"}}
			c.RequireAggregate = true
			c.AggregateValidator = func(ref agent.EngineeringContribution) ar.Diagnostics {
				if ref.SnapshotID != "aggregate" || !reflect.DeepEqual(ref.CheckIDs, []string{"aggregate-check"}) {
					t.Errorf("untouched reference lost: %+v", ref)
				}
				if fault == "stale aggregate" {
					return ar.Diagnostics{Complete: true, Diagnostics: []ar.Diagnostic{{Path: "$.result.payload.implement.aggregate_checks", Code: "evidence", Message: "Current worktree differs from retained aggregate."}}}
				}
				return ar.Diagnostics{Valid: true, Complete: true}
			}
			story := c.Stories["US-001"]
			check := story.Checks["check-1"]
			switch fault {
			case "unretained snapshot":
				story.SnapshotRetained = false
			case "missing check":
				delete(story.Checks, "check-1")
			case "wrong revision":
				check.Revision = "different-revision"
				story.Checks["check-1"] = check
			case "failed check":
				check.Passed = false
				story.Checks["check-1"] = check
			case "missing resolver":
				c.AggregateValidator = nil
			}
			c.Stories["US-001"] = story
			before := authored(t, s.Result)
			retired := ar.RetiredIDs{"result.artifacts": []string{"ART-old"}}
			got, d := ar.ApplyUpdate(s, retired, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "comment", "", `"New commentary only"`)}}, c, ar.EditTargets(c.Mode))
			if d.Valid != (fault == "valid") {
				t.Fatalf("evidence verdict: %+v", d)
			}
			if fault == "valid" {
				if got.Snapshot.Meta.ResultRevision != 2 || got.Snapshot.Result.Comment != "New commentary only" {
					t.Fatal("valid sparse update lost")
				}
			} else if !reflect.DeepEqual(got, ar.PreparedUpdate{}) || len(d.Diagnostics.Diagnostics) == 0 {
				t.Fatalf("invalid evidence yielded candidate or no diagnostic: %+v", d)
			}
			if !reflect.DeepEqual(before, authored(t, s.Result)) || !reflect.DeepEqual(retired, ar.RetiredIDs{"result.artifacts": []string{"ART-old"}}) {
				t.Fatal("validation changed retained state")
			}
		})
	}
}

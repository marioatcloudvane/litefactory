package turn

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestResultPlanContribution_PreservesCompleteProposal(t *testing.T) {
	t.Parallel()
	want := planbrief.TypedPlan{Summary: "Adopt shared final results", Stories: []planbrief.TypedStory{{
		Key: "US-001", Intent: "create", Title: "Finish through shared results",
		UserStory:          planbrief.UserStoryContent{Actor: "operator", Want: "reliable final results", Benefit: "recover interrupted work", Notes: []string{"Do not grant acceptance through a receipt."}},
		AcceptanceCriteria: []planbrief.CriterionContent{{ID: "AC-001", Text: "Missing final output prevents success."}},
		Tasks:              []planbrief.TaskContent{{ID: "T-001", Description: "Wire the completion gate", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}},
		Architecture:       planbrief.ArchitectureContent{Summary: "Reuse the result owner", Components: []planbrief.ComponentContent{{Name: "result service", Responsibility: "Validate results"}}, Interfaces: []planbrief.InterfaceContent{{Name: "finalize", Input: "result", Output: "receipt"}}, Constraints: []string{"Preserve owner authority"}, Decisions: []planbrief.DecisionContent{{Decision: "Use existing publication", Reason: "Avoid duplicate authority"}}},
		Dependencies:       planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{"github:example/project#263"}},
	}}}
	s := want.Stories[0]
	input := ar.Result{Outcome: "completed", Comment: "Proposal ready for existing Plan processing", Payload: ar.Payload{Plan: &ar.Plan{Summary: want.Summary, Stories: []ar.PlanStory{{Key: s.Key, Intent: s.Intent, Title: s.Title, UserStory: ar.UserStory{Actor: s.UserStory.Actor, Want: s.UserStory.Want, Benefit: s.UserStory.Benefit, Notes: s.UserStory.Notes}, AcceptanceCriteria: s.AcceptanceCriteria, Tasks: s.Tasks, Architecture: &s.Architecture, Dependencies: &s.Dependencies}}}}}

	got, err := resultPlanContribution(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != "plan" || got.Outcome != "completed" || got.Engineering != nil || got.Assessment != nil {
		t.Fatalf("proposal changed workflow authority: %+v", got)
	}
	var decoded planbrief.TypedPlan
	if err := json.Unmarshal(got.Plan, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("proposal lost typed requirements:\ngot: %#v\nwant: %#v", decoded, want)
	}
}

func TestResultPlanContribution_DiscussionCannotBecomeProposal(t *testing.T) {
	for _, outcome := range []string{"incomplete", "blocked"} {
		t.Run(outcome+"/retains context without publishable stories", func(t *testing.T) {
			t.Parallel()
			discussion := &agent.PlanningDiscussion{Scope: "Final route", Decisions: "Preserve existing owners", Assumptions: "Legacy readers remain", Questions: "Which cohort has drained?"}
			input := ar.Result{Outcome: outcome, Reason: "Compatibility evidence missing", Payload: ar.Payload{Plan: &ar.Plan{Context: discussion, Stories: []ar.PlanStory{{Key: "US-001", Title: "Unapproved draft", UserStory: ar.UserStory{Want: "retire old input"}}}}}}

			got, err := resultPlanContribution(input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stage != "plan" || got.Outcome != "discussion" || len(got.Plan) != 0 || !reflect.DeepEqual(got.Context, discussion) {
				t.Fatalf("discussion promoted or context lost: %+v", got)
			}
		})
	}
}

func TestResultPlanContribution_MissingPayload(t *testing.T) {
	t.Parallel()
	got, err := resultPlanContribution(ar.Result{Outcome: "completed", Comment: "All done"})
	if err == nil || got.Stage != "" || len(got.Plan) != 0 {
		t.Fatalf("missing Plan became usable contribution: %+v, %v", got, err)
	}
}

func TestResultDiagnosticError_PreservesCorrection(t *testing.T) {
	for _, tc := range []struct{ name, path, code, message string }{
		{"malformed field/actionable rejection", "$.result.comment", "type", "Supply a string comment."},
		{"missing evidence/actionable rejection", "$.result.payload.implement.aggregate_checks", "missing_evidence", "Reference retained aggregate checks."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := ar.Diagnostics{Complete: true, Diagnostics: []agent.ContributionDiagnostic{{Path: tc.path, Code: tc.code, Message: tc.message}}}
			got := resultDiagnosticError(d)
			if got == nil || !strings.Contains(got.Error(), tc.path) || !strings.Contains(got.Error(), tc.message) {
				t.Fatalf("rejection lost field or correction: %v", got)
			}
		})
	}
}

package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func typedPlanFixture(t *testing.T) (planbrief.TypedPlan, *preparation, *report.Reporter, planbrief.ContentScope) {
	t.Helper()
	const raw = `{"summary":"Factory-owned structure","stories":[{"key":"US-001","intent":"create","title":"Typed requirements","user_story":{"actor":"owner","want":"explicit requirements","benefit":"reliable evidence","notes":["Evidence contract: explanation only"]},"acceptance_criteria":[{"id":"AC-001","text":"String must be x"}],"tasks":[{"id":"T-001","description":"Implement requirement","owner_role":"go-implementation-engineer","criterion_ids":["AC-001"],"depends_on":[]}],"architecture":{"summary":"Pure contract","components":[],"interfaces":[],"constraints":[],"decisions":[]},"dependencies":{"story_keys":[],"issue_refs":[]}}]}`
	var p planbrief.TypedPlan
	mustProgress(t, json.Unmarshal([]byte(raw), &p))
	ref, err := naming.ParseRef("github:o/r#1")
	mustProgress(t, err)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	mustProgress(t, err)
	return p, &preparation{ref: ref, promptInput: harness.PromptInput{PlanGeneration: 3}}, renderer, planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}, IssueRefs: []string{}}
}
func typedPlanBytes(t *testing.T, p planbrief.TypedPlan) []byte {
	t.Helper()
	b, e := json.Marshal(p)
	mustProgress(t, e)
	return b
}

func TestTypedPlanFactoryBindingAndCompleteSet(t *testing.T) {
	p, prep, renderer, scope := typedPlanFixture(t)
	first, content, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, nil, nil, typedPlanBytes(t, p))
	mustProgress(t, err)
	again, _, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, nil, nil, typedPlanBytes(t, p))
	mustProgress(t, err)
	if !reflect.DeepEqual(first, again) || first.Parent != prep.ref.String() || first.Generation != 3 || len(content) != 1 {
		t.Fatalf("unstable factory proposal: %+v", first)
	}
	s := first.Stories[0]
	if s.IssueID != 0 || s.IssueNumber != 0 || s.ExpectedBase != "" {
		t.Fatal("new story acquired identity")
	}
	for _, heading := range []string{"User Story", "Acceptance Criteria", "Tasks", "Architecture", "Dependencies"} {
		if !strings.Contains(s.Body, "## "+heading) {
			t.Fatalf("lost %s", heading)
		}
	}
	if strings.Count(s.Body, "- [ ] AC-001:") != 1 {
		t.Fatalf("criterion rendering: %s", s.Body)
	}
	p.Stories[0].Intent = "update"
	bases := map[string]planbrief.StoryProposal{"US-001": {Key: "US-001", IssueNumber: 12, IssueID: 123, ExpectedBase: planbrief.StoryHash("old", "old body"), Title: "old", Body: "old body"}}
	bound, _, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, bases, nil, typedPlanBytes(t, p))
	mustProgress(t, err)
	if bound.Stories[0].IssueID != 123 || bound.Stories[0].IssueNumber != 12 || bound.Stories[0].ExpectedBase != bases["US-001"].ExpectedBase {
		t.Fatal("factory base lost")
	}
	for _, intent := range []string{"create", "unchanged"} {
		p.Stories[0].Intent = intent
		if _, _, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, bases, nil, typedPlanBytes(t, p)); err == nil {
			t.Fatalf("accepted %s against conflicting base", intent)
		}
	}
	p.Stories[0].Intent = "update"
	existing := []store.ManagedStory{{Key: "US-002"}}
	if _, _, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, bases, existing, typedPlanBytes(t, p)); err == nil {
		t.Fatal("omitted existing story accepted")
	}
}

func TestTypedPlanRejectsSemanticRedactionAndInjectedEnvelope(t *testing.T) {
	p, prep, renderer, scope := typedPlanFixture(t)
	p.Stories[0].AcceptanceCriteria[0].Text = "Use ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	if _, _, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, nil, nil, typedPlanBytes(t, p)); err == nil {
		t.Fatal("redacted requirement accepted")
	}
	p, prep, renderer, scope = typedPlanFixture(t)
	raw := strings.Replace(string(typedPlanBytes(t, p)), `"intent":"create"`, `"intent":"create","issue_id":123`, 1)
	if _, _, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, nil, nil, []byte(raw)); err == nil {
		t.Fatal("agent identity accepted")
	}
}

func TestTypedCriterionSourceAuthorityAndLegacyDispatch(t *testing.T) {
	p, _, _, _ := typedPlanFixture(t)
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, db.Close()) })
	mustProgress(t, db.Migrate(t.Context()))
	parent := "github:o/r#1"
	mustProgress(t, db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning"}))
	mustProgress(t, db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: parent, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	body := "## Acceptance Criteria\n- AC-001: String must be x\nEvidence contract: explanation, not an obligation\n"
	revision := planbrief.StoryHash(p.Stories[0].Title, body)
	if _, err := storyCriterionIDs(t.Context(), db, parent, 1, "US-001", revision, body); err == nil {
		t.Fatal("missing typed projection fell back to prose")
	}
	raw, err := json.Marshal(p.Stories[0])
	mustProgress(t, err)
	mustProgress(t, db.PutSubmissionProjection(t.Context(), store.SubmissionProjection{Parent: parent, Generation: 1, Story: "US-001", Revision: revision, Title: p.Stories[0].Title, Body: body, ContentVersion: 2, IssueID: 123, Content: raw, Criteria: []string{"AC-001"}}))
	ids, err := storyCriterionIDs(t.Context(), db, parent, 1, "US-001", revision, body)
	mustProgress(t, err)
	if !reflect.DeepEqual(ids, []string{"AC-001"}) {
		t.Fatalf("undeclared obligations: %v", ids)
	}
	if _, err := storyCriterionIDs(t.Context(), db, parent, 1, "US-001", revision, body+"owner edit"); err == nil {
		t.Fatal("stale projection overrode changed issue bytes")
	}
	if _, err := storyCriterionIDs(t.Context(), db, parent, 2, "US-001", revision, body); err == nil {
		t.Fatal("missing typed generation fell back to legacy")
	}
	legacy, err := storyCriterionIDs(t.Context(), db, "github:o/r#99", 1, "US-001", revision, "## Acceptance Criteria\n- legacy exact criterion\n")
	mustProgress(t, err)
	if !reflect.DeepEqual(legacy, []string{"legacy exact criterion"}) {
		t.Fatalf("legacy changed: %v", legacy)
	}
}

func TestTypedPlanSubmissionBoundArtifactsAndPendingReadiness(t *testing.T) {
	for _, outcome := range []string{"discussion", "completed"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t, nil)
			mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
			mustProgress(t, f.db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: f.session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
			p, prep, renderer, _ := typedPlanFixture(t)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			prep.ref = ref
			prep.session = f.session
			prep.session.FeatureSlug = "typed-plan"
			prep.session.Branch = "lf/1-typed-plan"
			prep.worktree = newSyncWorktree(t, f.repo, 77, "typed plan")
			prep.run = store.Run{Mode: "plan"}
			prep.promptInput = harness.PromptInput{PlanGeneration: 1, PlanProposalPath: planbrief.ProposalFileName, PlanContextPath: ".aiteam/test/PLAN_CONTEXT.json"}
			forge := &typedPlanForge{}
			callbacks, err := prepareTypedPlanSubmission(t.Context(), Options{DB: f.db, SubIssueCreator: forge, SubIssueRenderer: renderer}, prep)
			mustProgress(t, err)
			if callbacks == nil {
				t.Fatal("missing typed callbacks")
			}
			before, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			diagnostics := callbacks.Validate(t.Context(), typedPlanBytes(t, p))
			if !diagnostics.Valid || !diagnostics.Complete {
				t.Fatalf("valid preflight: %+v", diagnostics)
			}
			if _, err := os.Stat(filepath.Join(prep.worktree.Path(), planbrief.ProposalFileName)); !os.IsNotExist(err) {
				t.Fatal("preflight wrote proposal")
			}
			receipt := store.SubmissionReceipt{ID: 12, RunID: 31, Attempt: "factory-attempt"}
			contribution := agent.Contribution{Stage: "plan", Outcome: outcome, Plan: typedPlanBytes(t, p), Context: &agent.PlanningDiscussion{Scope: "Typed stories", Decisions: "Factory authority", Assumptions: "None", Questions: "None"}}
			mustProgress(t, callbacks.Submit(t.Context(), receipt, contribution))
			mustProgress(t, callbacks.Submit(t.Context(), receipt, contribution))
			receipt.ID++
			if err := callbacks.Submit(t.Context(), receipt, contribution); err == nil {
				t.Fatal("different request changed accepted outcome")
			}
			outcomeBytes, err := os.ReadFile(filepath.Join(prep.worktree.Path(), planbrief.OutcomeFileName))
			mustProgress(t, err)
			var got planbrief.PlanOutcome
			mustProgress(t, json.Unmarshal(outcomeBytes, &got))
			if got.Parent != f.session.TaskRef || got.RunID != 31 || got.Attempt != "factory-attempt" || got.Generation != 1 || got.Outcome != outcome {
				t.Fatalf("wrong envelope: %+v", got)
			}
			proposal, err := os.ReadFile(filepath.Join(prep.worktree.Path(), planbrief.ProposalFileName))
			if outcome == "discussion" {
				if !os.IsNotExist(err) {
					t.Fatal("discussion wrote proposal")
				}
				contextBytes, err := os.ReadFile(filepath.Join(prep.worktree.Path(), prep.promptInput.PlanContextPath))
				mustProgress(t, err)
				if !strings.Contains(string(contextBytes), f.session.TaskRef) {
					t.Fatal("discussion unbound")
				}
			} else {
				mustProgress(t, err)
			}
			mustProgress(t, prep.typedPlan.checkTransport(outcomeBytes, proposal))
			if err := prep.typedPlan.checkTransport(append(outcomeBytes, ' '), proposal); err == nil {
				t.Fatal("mutated transport trusted")
			}
			after, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			if !reflect.DeepEqual(before, after) || forge.creates != 0 || forge.updates != 0 {
				t.Fatal("input recording published or restored readiness")
			}
		})
	}
}

type typedPlanForge struct{ publicationForge }

func (*typedPlanForge) CreateSubIssues(context.Context, github.Repo, int, []github.SubIssueSpec) ([]github.SubIssueResult, error) {
	panic("unexpected legacy subissue publication")
}

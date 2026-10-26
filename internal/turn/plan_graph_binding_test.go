package turn

import (
	"context"
	"slices"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

const graphBindingDraft = `{"key":"story-001","name":"Add export","user_story":{"actor":"owner","want":"export data","benefit":"share it","notes":[]},"problem":"No export exists.","in_scope":["export"],"not_in_scope":[],"dependencies":{"story_keys":[],"issue_refs":[]},"frontend":false}`

func TestPlanGraphBinding_FactoryValuesComeFromState(t *testing.T) {
	state := planbrief.PlanState{SharedRevision: "shared-7"}
	binding, err := planGraphBinding(state, "story-001", "story", 4, 9, "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Story != "story-001" || binding.Stage != "story" || binding.Candidate != "plan-4-9" || binding.RequestID != "plan-request-4-9" || binding.SharedRevision != "shared-7" || binding.ExistingRevision != "" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	next, err := planGraphBinding(state, "story-001", "story", 4, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.Candidate == binding.Candidate || next.RequestID == binding.RequestID {
		t.Fatal("distinct attempts share request identity")
	}
	if !slices.Contains(planGraphResultTargets(), ar.Target{Field: "payload.plan.storyrequest"}) || !slices.Contains(planGraphResultTargets(), ar.Target{Field: "payload.plan.summary"}) {
		t.Fatal("graph Plan grants are not the legacy targets plus the branches")
	}
}

// One resolver serves preflight and submit, so both run the same validation, and
// a state change between them refuses bytes preflight accepted without retaining
// them. Preflight itself allocates nothing.
func TestAgentResultService_GraphPlanPreflightAndSubmitShareValidation(t *testing.T) {
	db, _, o := agentResultFixture(t, "plan")
	binding := &ar.PlanGraphBinding{Story: "story-001", Stage: "story", Candidate: "plan-1-1", RequestID: "plan-request-1-1", Upstream: []string{}, SharedRevision: "initial"}
	o.Resolve = func(context.Context) (ar.Context, error) {
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}, PlanGraph: binding.Clone()}, nil
	}
	o.ReadTargets, o.EditTargets = planGraphResultTargets(), planGraphResultTargets()
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"result":{"outcome":"completed","comment":"Proposed.","payload":{"plan":{"storyrequest":{"operation":"create","draft":` + graphBindingDraft + `}}}}}`)
	restricted := context.WithValue(t.Context(), mcpCommunicationKey{}, true)
	first, err := service.Preflight(restricted, data)
	if err != nil || !first.Valid {
		t.Fatalf("preflight refused a graph Plan story request: %+v %v", first, err)
	}
	if revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatal("preflight retained a revision")
	}
	again, err := service.Preflight(restricted, data)
	if err != nil || again.Valid != first.Valid || len(again.Diagnostics.Diagnostics) != len(first.Diagnostics.Diagnostics) {
		t.Fatal("identical bytes changed outcome with unchanged state")
	}
	// The story acquired an issue draft after preflight: the same bytes are stale.
	binding.ExistingRevision = strings.Repeat("c", 64)
	refused, err := service.Submit(restricted, "req-1", data)
	if err != nil {
		t.Fatal(err)
	}
	if refused.Validation.Valid || refused.Snapshot != nil {
		t.Fatalf("stale content accepted at submit: %+v", refused.Validation)
	}
	if revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatal("stale content retained")
	}
	binding.ExistingRevision = ""
	accepted, err := service.Submit(restricted, "req-2", data)
	if err != nil || !accepted.Validation.Valid || accepted.Snapshot == nil {
		t.Fatalf("submit refused: %+v %v", accepted.Validation, err)
	}
	q := accepted.Snapshot.Result.Payload.Plan.StoryRequest
	if q.Stage != "story" || q.Candidate != "plan-1-1" || q.RequestID != "plan-request-1-1" || q.SharedRevision != "initial" {
		t.Fatalf("recorded values are not the factory's: %+v", q)
	}
	supplied := []byte(strings.Replace(string(data), `"operation":"create",`, `"operation":"create","stage":"technical",`, 1))
	if d, err := service.Preflight(restricted, supplied); err != nil || d.Valid {
		t.Fatalf("supplied factory field accepted at preflight: %+v %v", d, err)
	}
}

// #1160 c5 (#1172): an upstream story that gets a new candidate while a
// dependant runs, without changing what the dependant's stage is reviewed
// against, no longer stales the dependant's result.
func TestPlanGraphBinding_UpstreamCandidateMoveKeepsTheResult(t *testing.T) {
	f, _, base, forge, u := upstreamFixture(t)
	u.after = func(s planAgentStep) {
		if s.story != "story-001" {
			return
		}
		mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(st *planbrief.PlanState) (bool, error) {
			for _, item := range st.Stories {
				if item.Key == "story-002" {
					changed := item.Draft
					changed.Criteria = slices.Clone(changed.Criteria)
					if len(changed.Criteria) == 0 {
						t.Fatal("story-002 has no acceptance content")
					}
					changed.Criteria[0].Text = "A revised acceptance criterion"
					return true, st.PutCandidate(changed, "acceptance", "upstream-acceptance-2")
				}
			}
			return false, nil
		}))
	}
	before := indepStory(t, f, "story-001")
	executeGraphPlan(t, f, planWorkRun(t, f, base, "story-001"))
	if bodies := failureBodies(forge); len(bodies) != 0 {
		t.Fatalf("an unrelated upstream candidate move failed the dependant: %q", bodies)
	}
	item := indepStory(t, f, "story-001")
	if item.Candidate == before.Candidate || item.Wait == nil || item.Wait.Stage != "technical" || item.Draft.Architecture == nil {
		t.Fatalf("the dependant's result was not applied and reviewed: %+v", item)
	}
	if upstream := indepStory(t, f, "story-002"); upstream.Candidate != "upstream-acceptance-2" {
		t.Fatalf("the upstream change was lost: %+v", upstream)
	}
}

// #1160 c12: an owner edit of the story itself while its agent runs is never
// re-bound: the result is refused as stale, the owner is told, and the edit stays.
func TestPlanGraphBinding_OwnerEditMidRunIsNotRebound(t *testing.T) {
	f, remote, base, forge, u := upstreamFixture(t)
	u.after = func(s planAgentStep) {
		if s.story != "story-001" {
			return
		}
		binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
		mustProgress(t, err)
		remote.mu.Lock()
		body, _ := remote.children[binding.IssueNumber]["body"].(string)
		if !strings.Contains(body, "I want shape intent") {
			remote.mu.Unlock()
			t.Fatalf("the fixture's sub-issue lacks the expected intent: %q", body)
		}
		remote.children[binding.IssueNumber]["body"] = strings.Replace(body, "I want shape intent", "I want an owner-edited intent", 1)
		remote.mu.Unlock()
		client, ok := f.opts.SubIssueCreator.(PlanStoryForge)
		if !ok {
			t.Fatal("fixture requires PlanStoryForge")
		}
		issue, err := client.GetIssue(t.Context(), github.Repo{Owner: "acme", Name: "acme-api"}, binding.IssueNumber)
		mustProgress(t, err)
		mustProgress(t, ReconcilePlanChildEdit(t.Context(), f.db, f.ref().String(), 123, 1230, issue, planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}))
	}
	out, _ := Execute(t.Context(), f.opts, planWorkRun(t, f, base, "story-001"))
	requireFailureComment(t, forge, "storyrequest.upstream")
	if out.Run.Status != store.RunFailed {
		t.Fatalf("a stale result finished nominally: %+v", out.Run)
	}
	item := indepStory(t, f, "story-001")
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	if !strings.HasPrefix(item.Candidate, "owner-edit-") || binding.Candidate != item.Candidate || item.Draft.UserStory.Want != "an owner-edited intent" || item.Wait != nil {
		t.Fatalf("the owner's edit was not kept: state %+v binding %+v", item, binding)
	}
}

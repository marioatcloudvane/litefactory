package planbrief_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func workflowFixture(t *testing.T, keys ...string) planbrief.PlanState {
	t.Helper()
	s := planbrief.PlanState{SharedRevision: "shared-1"}
	for _, key := range keys {
		d := draftFixture("acceptance")
		d.Key = key
		if err := s.PutCandidate(d, "story", key+"-candidate"); err != nil {
			t.Fatal(err)
		}
	}
	return s
}
func workflowReply(t *testing.T, s *planbrief.PlanState, index int, id int64) planbrief.PlanOwnerReply {
	t.Helper()
	st := s.Stories[index]
	if err := s.OpenPlanWait(st.Key, "wait-"+st.Stage, "review", int64(100+index)); err != nil {
		t.Fatal(err)
	}
	return planbrief.PlanOwnerReply{ID: id, OwnerID: 7, ActorID: 7, IssueID: int64(100 + index), Key: st.Key, Stage: st.Stage, Candidate: st.Candidate, WaitID: "wait-" + st.Stage, Body: "/approve-plan " + st.Key + " " + st.Stage + " " + st.Candidate}
}
func approveWorkflowStory(t *testing.T, s *planbrief.PlanState, index int, base int64) {
	t.Helper()
	for n := range 3 {
		r := workflowReply(t, s, index, base+int64(n))
		ok, err := s.ApplyOwnerReply(r)
		if err != nil || !ok {
			t.Fatalf("approve %s: %v %v", r.Stage, ok, err)
		}
	}
}
func cloneWorkflow(t *testing.T, s planbrief.PlanState) planbrief.PlanState {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out planbrief.PlanState
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPlanWorkflow_IndependentProgress(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a", "b", "c")
	if err := s.OpenPlanWait("a", "actor-question", "question", 100); err != nil {
		t.Fatal(err)
	}
	approveWorkflowStory(t, &s, 1, 10)
	approveWorkflowStory(t, &s, 2, 20)
	if s.Stories[0].Stage != "story" || s.Stories[0].Wait == nil || len(s.Stories[0].Approvals) != 0 {
		t.Fatal("waiting story was promoted")
	}
	for _, i := range []int{1, 2} {
		if s.Stories[i].Stage != "acceptance" || len(s.Stories[i].Approvals) != 3 {
			t.Fatalf("independent story did not advance: %+v", s.Stories[i])
		}
	}
	if slices.Contains(s.EligiblePlanStories(), "a") {
		t.Fatal("waiting story selected")
	}
	if got := s.EligiblePlanStories(); len(got) != 0 {
		t.Fatalf("approved acceptance stories were selected again: %v", got)
	}
}

func TestPlanWorkflow_ExactOwnerReply(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*planbrief.PlanOwnerReply)
		question bool
	}{
		{"foreign owner", func(r *planbrief.PlanOwnerReply) { r.ActorID = 8 }, false},
		{"wrong story", func(r *planbrief.PlanOwnerReply) { r.Key = "b" }, false},
		{"wrong child", func(r *planbrief.PlanOwnerReply) { r.IssueID++ }, false},
		{"stale candidate", func(r *planbrief.PlanOwnerReply) { r.Candidate = "old" }, false},
		{"wrong stage", func(r *planbrief.PlanOwnerReply) { r.Stage = "technical" }, false},
		{"wrong wait", func(r *planbrief.PlanOwnerReply) { r.WaitID = "old-wait" }, false},
		{"ambiguous prose", func(r *planbrief.PlanOwnerReply) { r.Body = "Looks fine" }, false},
		{"mixed change and approval", func(r *planbrief.PlanOwnerReply) { r.Body += " but change actor" }, false},
		{"question with open questions cannot promote", func(r *planbrief.PlanOwnerReply) {}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b")
			r := workflowReply(t, &s, 0, 11)
			tt.change(&r)
			if tt.question {
				s.Stories[0].Wait.Kind = "question"
				s.Stories[0].Draft.Questions = []string{"Which owner?"}
			}
			ok, _ := s.ApplyOwnerReply(r)
			if ok || len(s.Stories[0].Approvals) != 0 || s.Stories[0].Stage != "story" {
				t.Fatal("unbound reply promoted")
			}
		})
	}
	t.Run("exact reply replay", func(t *testing.T) {
		s := workflowFixture(t, "a")
		r := workflowReply(t, &s, 0, 11)
		ok, err := s.ApplyOwnerReply(r)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		ok, err = s.ApplyOwnerReply(r)
		if err != nil || ok || s.Stories[0].Stage != "technical" || len(s.ConsumedReplies) != 1 {
			t.Fatalf("reply replay changed state: %v %v %+v", ok, err, s)
		}
	})
}

func TestPlanWorkflow_DependencyInvalidation(t *testing.T) {
	for _, stage := range []string{"story", "technical"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b", "c")
			s.Stories[1].Draft.Dependencies.StoryKeys = []string{"a"}
			for i := range s.Stories {
				approveWorkflowStory(t, &s, i, int64(10+i*10))
			}
			before := cloneWorkflow(t, s)
			changed := s.Stories[0].Draft
			if stage == "story" {
				changed.UserStory.Want = "revised need"
			} else {
				raw, _ := json.Marshal(changed.Architecture)
				var arch planbrief.PlanArchitecture
				if err := json.Unmarshal(raw, &arch); err != nil {
					t.Fatal(err)
				}
				changed.Architecture = &arch
				changed.Architecture.General[0].Decision = "revised contract"
			}
			if err := s.PutCandidate(changed, stage, "new-candidate"); err != nil {
				t.Fatal(err)
			}
			for _, i := range []int{0, 1} {
				if s.Stories[i].Stage != stage || s.Stories[i].Approvals[stage] != "" || s.Stories[i].Approvals["acceptance"] != "" {
					t.Fatalf("affected story retained stale approval: %+v", s.Stories[i])
				}
				if stage == "technical" && s.Stories[i].Approvals["story"] == "" {
					t.Fatal("technical change discarded approved intent")
				}
			}
			if !reflect.DeepEqual(s.Stories[2], before.Stories[2]) {
				t.Fatal("unrelated story changed")
			}
		})
	}
}

func TestPlanWorkflow_SharedAndPresentation(t *testing.T) {
	s := workflowFixture(t, "a")
	approveWorkflowStory(t, &s, 0, 10)
	before := cloneWorkflow(t, s)
	if err := s.PutCandidate(s.Stories[0].Draft, "acceptance", "presentation-candidate"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Stories[0].Approvals, before.Stories[0].Approvals) {
		t.Fatal("identical canonical content invalidated approval")
	}
	if err := s.SetSharedRevision("shared-2"); err != nil {
		t.Fatal(err)
	}
	if len(s.Stories[0].Approvals) != 0 || s.Stories[0].Stage != "story" {
		t.Fatal("shared change preserved stale approval")
	}
}

func TestPlanWorkflow_CompleteAndReadiness(t *testing.T) {
	scope := planbrief.ContentScope{Roles: []string{"engineer"}}
	t.Run("approval publication save separate", func(t *testing.T) {
		s := workflowFixture(t, "a")
		approveWorkflowStory(t, &s, 0, 10)
		ref, err := s.OpenAssembledReview(scope)
		if err != nil {
			t.Fatal(err)
		}
		if s.PlanReady(scope) {
			t.Fatal("local approvals are readiness")
		}
		if err = s.ApproveAssembled(7, 7, 20, "/approve-plan-set "+ref, scope); err != nil {
			t.Fatal(err)
		}
		if s.PlanReady(scope) {
			t.Fatal("owner approval is publication")
		}
		s.PublishedRevision = ref
		if s.PlanReady(scope) {
			t.Fatal("publication is saved readiness")
		}
		s.SavedRevision = ref
		if !s.PlanReady(scope) {
			t.Fatal("confirmed approved publication/save not ready")
		}
		s = cloneWorkflow(t, s)
		if !s.PlanReady(scope) {
			t.Fatal("restart lost readiness")
		}
	})
	tests := []struct {
		name   string
		change func(*planbrief.PlanState)
	}{
		{"empty scope", func(s *planbrief.PlanState) { s.Stories = nil }},
		{"cycle", func(s *planbrief.PlanState) { s.Stories[0].Draft.Dependencies.StoryKeys = []string{"a"} }},
		{"dangling", func(s *planbrief.PlanState) { s.Stories[0].Draft.Dependencies.StoryKeys = []string{"missing"} }},
		{"retired prerequisite", func(s *planbrief.PlanState) {
			s.Stories[0].Draft.Dependencies.StoryKeys = []string{"b"}
			s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: "b", Retired: true})
		}},
		{"material question", func(s *planbrief.PlanState) { s.Stories[0].Draft.Questions = []string{"Which owner?"} }},
		{"uncertain effect", func(s *planbrief.PlanState) { s.Stories[0].Effect = "uncertain" }},
		{"pending edit", func(s *planbrief.PlanState) { s.Stories[0].Hold = "unreconciled edit" }},
		{"missing General", func(s *planbrief.PlanState) { s.Stories[0].Draft.Architecture.General = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := workflowFixture(t, "a")
			approveWorkflowStory(t, &s, 0, 10)
			tt.change(&s)
			if err := s.ValidateComplete(scope); err == nil {
				t.Fatal("incomplete Plan accepted")
			}
		})
	}
}

func TestPlanWorkflow_MixedGoAndNeutralOwnersAssemble(t *testing.T) {
	scope := planbrief.ReleaseScope([]string{"implementation-engineer", "unit-test-engineer", "integration-test-engineer"}, []string{"agent-setup"})
	s := planbrief.PlanState{SharedRevision: "shared-1"}
	for key, owner := range map[string]string{"a": "go-implementation-engineer", "b": "implementation-engineer", "c": "go-unit-test-engineer"} {
		d := draftFixture("acceptance")
		d.Key = key
		d.Tasks[0].OwnerRole = owner
		if err := s.PutCandidate(d, "story", key+"-candidate"); err != nil {
			t.Fatal(err)
		}
	}
	for i := range s.Stories {
		approveWorkflowStory(t, &s, i, int64(10+10*i))
	}
	if err := s.ValidateComplete(scope); err != nil {
		t.Fatal("mixed owners incomplete:", err)
	}
	if ref, due := s.AssemblyDue(scope); !due || ref == "" {
		t.Fatal("assembled review not due")
	}
	drafts := make([]planbrief.PlanDraft, 0, len(s.Stories))
	for _, item := range s.Stories {
		drafts = append(drafts, item.Draft)
	}
	if _, err := planbrief.CompletePlanDrafts("summary", drafts, scope); err != nil {
		t.Fatal("finalize rejected mixed owners:", err)
	}
	s.Stories[0].Draft.Tasks[0].OwnerRole = "go-frontend-engineer"
	if err := s.ValidateComplete(scope); err == nil || !strings.Contains(err.Error(), "authorized role") {
		t.Fatal("unknown go- owner accepted:", err)
	}
}

func TestPlanWorkflow_Retirement(t *testing.T) {
	s := workflowFixture(t, "a", "b")
	if err := s.OpenPlanWait("a", "retire-review", "retire", 100); err != nil {
		t.Fatal(err)
	}
	r := planbrief.PlanOwnerReply{ID: 10, OwnerID: 7, ActorID: 7, IssueID: 100, Key: "a", Stage: "story", Candidate: "a-candidate", WaitID: "retire-review", Body: "/retire-plan a a-candidate"}
	bad := r
	bad.Body = "Architecture recommends retirement"
	if s.ConfirmRetirement(bad) == nil {
		t.Fatal("architecture advice retired scope")
	}
	if err := s.ConfirmRetirement(r); err != nil {
		t.Fatal(err)
	}
	if s.Stories[0].Retired {
		t.Fatal("confirmation alone claimed remote closure")
	}
	if err := s.MarkRetired("a", []string{"b"}, r.ID); err != nil {
		t.Fatal(err)
	}
	if !s.Stories[0].Retired || s.Stories[0].Candidate != "a-candidate" || !reflect.DeepEqual(s.Stories[0].Replacements, []string{"b"}) {
		t.Fatal("retirement lost identity/history")
	}
}

func TestPlanWorkflow_RetainedArchitectureAndTypedHandoff(t *testing.T) {
	for _, kind := range []string{"agent", "go", "frontend"} {
		t.Run(kind, func(t *testing.T) {
			d := draftFixture("acceptance")
			if kind == "go" {
				d.Guidance = []string{"go"}
				d.Architecture.ServiceDesign = []planbrief.ServiceDefinition{{Component: "Repository", Responsibility: "Own storage", Access: "Database"}}
			}
			if kind == "frontend" {
				d.Frontend = true
				d.Design = []string{"https://example.com/design"}
			}
			if err := d.ValidateRetained("story", planbrief.ContentScope{Roles: []string{"engineer"}}); err != nil {
				t.Fatal(err)
			}
			if d.Validate("story", planbrief.ContentScope{}) == nil {
				t.Fatal("initial strict draft validation weakened")
			}
			got, err := planbrief.CompletePlanDrafts("Scoped Plan", []planbrief.PlanDraft{d}, planbrief.ContentScope{Roles: []string{"engineer"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Stories) != 1 || got.Stories[0].AcceptanceCriteria[0].ID != "AC-001" || got.Stories[0].Tasks[0].CriterionIDs[0] != "AC-001" {
				t.Fatal("handoff lost criteria links")
			}
			raw, _ := json.Marshal(got)
			for _, want := range []string{d.Problem, d.Architecture.General[0].Decision, d.Criteria[0].ProofMethod} {
				if !strings.Contains(string(raw), want) {
					t.Fatalf("handoff lost %q", want)
				}
			}
			if kind == "frontend" && !strings.Contains(string(raw), d.Design[0]) {
				t.Fatal("handoff lost frontend reference")
			}
			s := workflowFixture(t, "a")
			d.Key = "a"
			if err := s.PutCandidate(d, "acceptance", "review"); err != nil {
				t.Fatal(err)
			}
			before := cloneWorkflow(t, s)
			s = cloneWorkflow(t, s)
			if err := s.PutCandidate(s.Stories[0].Draft, "acceptance", "again"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.Stories[0].Draft, before.Stories[0].Draft) {
				t.Fatal("repeat/restart reset settled architecture")
			}
		})
	}
}

func TestPlanWorkflow_ChangedInputsRejectDisplayedApproval(t *testing.T) {
	for _, kind := range []string{"dependency", "shared", "upstream unapproved"} {
		t.Run(kind, func(t *testing.T) {
			s := workflowFixture(t, "a", "b")
			s.Stories[0].Draft.Dependencies.StoryKeys = []string{"b"}
			if kind == "upstream unapproved" {
				s.Stories[0].Stage = "technical"
			}
			r := workflowReply(t, &s, 0, 11)
			switch kind {
			case "dependency":
				s.Stories[1].Draft.UserStory.Want = "a different need"
			case "shared":
				s.SharedRevision = "new-shared"
			}
			promoted, err := s.ApplyOwnerReply(r)
			if err == nil || promoted || len(s.Stories[0].Approvals) != 0 {
				t.Fatal("changed or unapproved inputs accepted", promoted, err)
			}
			if slices.Contains(s.ConsumedReplies, r.ID) {
				t.Fatal("rejected approval consumed reply")
			}
		})
	}
}

func TestPlanWorkflow_RetirementRejectsStaleAndForeign(t *testing.T) {
	for _, kind := range []string{"foreign owner", "wrong issue", "stale candidate", "mixed request", "changed dependency"} {
		t.Run(kind, func(t *testing.T) {
			s := workflowFixture(t, "a", "b")
			s.Stories[0].Draft.Dependencies.StoryKeys = []string{"b"}
			if err := s.OpenPlanWait("a", "retire", "retire", 100); err != nil {
				t.Fatal(err)
			}
			r := planbrief.PlanOwnerReply{ID: 11, OwnerID: 7, ActorID: 7, IssueID: 100, Key: "a", Stage: "story", Candidate: "a-candidate", WaitID: "retire", Body: "/retire-plan a a-candidate"}
			switch kind {
			case "foreign owner":
				r.ActorID = 8
			case "wrong issue":
				r.IssueID = 200
			case "stale candidate":
				r.Candidate = "old"
			case "mixed request":
				r.Body += " and modify b"
			case "changed dependency":
				s.Stories[1].Draft.Problem = "Changed scope"
			}
			if err := s.ConfirmRetirement(r); err == nil {
				t.Fatal("unconfirmed retirement accepted")
			}
			if s.Stories[0].Retired {
				t.Fatal("rejected retirement mutated state")
			}
		})
	}
}

func TestPlanWorkflowInlineApproval(t *testing.T) {
	for _, suffix := range []string{"", "\r\nchange it"} {
		s := workflowFixture(t, "a")
		reply := workflowReply(t, &s, 0, 10)
		reply.Body = " \n`" + reply.Body + "`" + suffix
		promoted, err := s.ApplyOwnerReply(reply)
		if err != nil || promoted != (suffix == "") {
			t.Fatalf("suffix %q: promoted=%t err=%v", suffix, promoted, err)
		}
	}
}

// strandedWorkflow builds the MAR-13 shape: story-002 depends on a fully
// approved story-001, sits at technical with an applied candidate, and has lost
// every approval with no review waiting.
func strandedWorkflow(t *testing.T) planbrief.PlanState {
	t.Helper()
	s := workflowFixture(t, "story-001", "story-002")
	s.Stories[1].Draft.Dependencies.StoryKeys = []string{"story-001"}
	approveWorkflowStory(t, &s, 0, 10)
	r := workflowReply(t, &s, 1, 20)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve story-002 story: %v %v", ok, err)
	}
	if s.Stories[1].Stage != "technical" || s.Stories[1].Wait != nil {
		t.Fatalf("story-002 not at technical: %+v", s.Stories[1])
	}
	s.Stories[1].Approvals = map[string]string{}
	return s
}

func TestPlanWorkflow_StrandedStoryReoffersEarliestUnapprovedStage(t *testing.T) {
	t.Parallel()
	s := strandedWorkflow(t)
	if slices.Contains(s.EligiblePlanStories(), "story-002") {
		t.Fatal("stranded story became eligible at its current stage")
	}
	stage, create, ok := s.StoryNextStep("story-002")
	if stage != "story" || create || !ok {
		t.Fatalf("StoryNextStep = (%q, %v, %v), want (story, false, true)", stage, create, ok)
	}

	// Re-running the story stage lowers the stage, opens a review at story and
	// an exact approval moves it to technical again.
	if err := s.PutCandidate(s.Stories[1].Draft, "story", "story-002-rerun"); err != nil {
		t.Fatal(err)
	}
	if s.Stories[1].Stage != "story" {
		t.Fatalf("rerun did not lower the stage: %+v", s.Stories[1])
	}
	r := workflowReply(t, &s, 1, 40)
	if s.Stories[1].Wait == nil || s.Stories[1].Wait.Stage != "story" || !strings.Contains(r.Body, "/approve-plan story-002 story story-002-rerun") {
		t.Fatalf("review wait not at story: %+v %q", s.Stories[1].Wait, r.Body)
	}
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve rerun: %v %v", ok, err)
	}
	if s.Stories[1].Stage != "technical" || s.Stories[1].Approvals["story"] == "" {
		t.Fatalf("approval did not recover the story: %+v", s.Stories[1])
	}
	if stage, _, ok := s.StoryNextStep("story-002"); stage != "technical" || !ok {
		t.Fatalf("recovered story next step = (%q, %v)", stage, ok)
	}
}

func TestPlanWorkflow_StrandedStoryRuleLeavesOtherStatesAlone(t *testing.T) {
	t.Parallel()
	t.Run("current approvals keep the current stage", func(t *testing.T) {
		t.Parallel()
		s := workflowFixture(t, "story-001", "story-002")
		s.Stories[1].Draft.Dependencies.StoryKeys = []string{"story-001"}
		approveWorkflowStory(t, &s, 0, 10)
		r := workflowReply(t, &s, 1, 20)
		if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if stage, create, ok := s.StoryNextStep("story-002"); stage != "technical" || create || !ok {
			t.Fatalf("StoryNextStep = (%q, %v, %v)", stage, create, ok)
		}
	})
	cases := map[string]func(*planbrief.PlanState){
		// OpenPlanWait opens no wait behind a stale earlier approval (#1160),
		// so a wait saved before that rule is set directly.
		"waiting": func(s *planbrief.PlanState) {
			s.Stories[1].Wait = &planbrief.PlanWait{ID: "wait-q", Stage: "technical", Candidate: s.Stories[1].Candidate, Kind: "question", IssueID: 101}
		},
		"effect held":           func(s *planbrief.PlanState) { s.Stories[1].EffectHold = "held" },
		"upstream not approved": func(s *planbrief.PlanState) { delete(s.Stories[0].Approvals, "story") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := strandedWorkflow(t)
			change(&s)
			if stage, create, ok := s.StoryNextStep("story-002"); ok {
				t.Fatalf("StoryNextStep = (%q, %v, %v), want not ok", stage, create, ok)
			}
		})
	}
}

func questionWaitReply(t *testing.T, s *planbrief.PlanState) planbrief.PlanOwnerReply {
	t.Helper()
	for n := range 2 {
		r := workflowReply(t, s, 0, int64(10+n))
		if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
			t.Fatalf("approve %s: %v %v", r.Stage, ok, err)
		}
	}
	const id = "plan-review:1013:9915:acceptance_ask"
	if err := s.OpenPlanWait("a", id, "question", 100); err != nil {
		t.Fatal(err)
	}
	st := s.Stories[0]
	return planbrief.PlanOwnerReply{ID: 50, OwnerID: 7, ActorID: 7, IssueID: 100, Key: "a", Stage: st.Stage, Candidate: st.Candidate, WaitID: id, Body: "/approve-plan a acceptance " + st.Candidate}
}

func TestPlanWorkflow_EmptyQuestionWaitAcceptsExactApproval(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a", "b")
	r := questionWaitReply(t, &s)
	if s.Stories[0].Stage != "acceptance" || s.Stories[0].Wait.Kind != "question" {
		t.Fatalf("fixture is not an acceptance question wait: %+v", s.Stories[0])
	}
	ref, err := s.ReviewRevision("a", "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.ApplyOwnerReply(r)
	if err != nil || !ok {
		t.Fatalf("exact approval against question wait: %v %v", ok, err)
	}
	st := s.Stories[0]
	if st.Approvals["acceptance"] != ref || st.Wait != nil || !slices.Contains(s.ConsumedReplies, r.ID) {
		t.Fatalf("approval not applied like a review: %+v consumed=%v", st, s.ConsumedReplies)
	}
}

func TestPlanWorkflow_QuestionWaitKeepsExactMatching(t *testing.T) {
	t.Parallel()
	t.Run("mismatched candidate", func(t *testing.T) {
		s := workflowFixture(t, "a", "b")
		r := questionWaitReply(t, &s)
		r.Candidate, r.Body = "old", "/approve-plan a acceptance old"
		if ok, err := s.ApplyOwnerReply(r); ok || err == nil || !strings.Contains(err.Error(), "stale or wrong-story") {
			t.Fatalf("mismatched candidate: %v %v", ok, err)
		}
	})
	t.Run("shared revision changed", func(t *testing.T) {
		s := workflowFixture(t, "a", "b")
		r := questionWaitReply(t, &s)
		s.SharedRevision = "new-shared"
		if ok, err := s.ApplyOwnerReply(r); ok || err == nil || !strings.Contains(err.Error(), "dependencies or shared decisions changed") {
			t.Fatalf("stale revision: %v %v", ok, err)
		}
		if _, ok := s.Stories[0].Approvals["acceptance"]; ok {
			t.Fatal("stale reply approved")
		}
	})
	t.Run("prose stays feedback", func(t *testing.T) {
		s := workflowFixture(t, "a", "b")
		r := questionWaitReply(t, &s)
		r.Body = "Please add a retry task"
		ok, err := s.ApplyOwnerReply(r)
		if ok || err != nil || s.Stories[0].Wait != nil || !slices.Contains(s.ConsumedReplies, r.ID) {
			t.Fatalf("prose reply: %v %v %+v", ok, err, s.Stories[0])
		}
		if _, ok := s.Stories[0].Approvals["acceptance"]; ok {
			t.Fatal("prose reply approved")
		}
	})
	t.Run("missing stage content", func(t *testing.T) {
		s := workflowFixture(t, "a", "b")
		s.Stories[0].Draft.Tasks, s.Stories[0].Draft.Criteria = nil, nil
		r := questionWaitReply(t, &s)
		ok, err := s.ApplyOwnerReply(r)
		if ok || err != nil {
			t.Fatalf("question wait without acceptance content: %v %v", ok, err)
		}
		if _, ok := s.Stories[0].Approvals["acceptance"]; ok {
			t.Fatal("question wait without content approved")
		}
	})
}

// dependantFixture has story-002 depend on story-001, with story-001's story
// stage approved and story-002 drafted at its story stage.
func dependantFixture(t *testing.T) planbrief.PlanState {
	t.Helper()
	s := workflowFixture(t, "story-001", "story-002")
	s.Stories[1].Draft.Dependencies.StoryKeys = []string{"story-001"}
	r := workflowReply(t, &s, 0, 10)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve story-001 story: %v %v", ok, err)
	}
	return s
}

func TestPlanWorkflow_UpstreamPublishingIsItsOwnError(t *testing.T) {
	t.Parallel()
	s := dependantFixture(t)
	s.Stories[0].Effect, s.Stories[0].EffectRequest = "pending", "plan-request-1-2"
	_, err := s.ReviewRevision("story-002", "story")
	if !errors.Is(err, planbrief.ErrPlanUpstreamPublishing) || err.Error() != "plan dependency story-001 is still publishing its stage" {
		t.Fatalf("publishing upstream error = %v", err)
	}
	if _, err := s.UpstreamReferences("story-002", "story"); !errors.Is(err, planbrief.ErrPlanUpstreamPublishing) {
		t.Fatalf("UpstreamReferences error = %v", err)
	}
	// The story's own publication, and every real hold, keep the old error.
	if _, err := s.ReviewRevision("story-001", "story"); err == nil || errors.Is(err, planbrief.ErrPlanUpstreamPublishing) || err.Error() != "plan dependency story-001 is inactive or unresolved" {
		t.Fatalf("own publication error = %v", err)
	}
	holds := map[string]func(*planbrief.PlanStoryState){
		"hold":           func(st *planbrief.PlanStoryState) { st.Hold = "owner edit conflict" },
		"lifecycle hold": func(st *planbrief.PlanStoryState) { st.LifecycleHold = "unavailable" },
		"effect hold":    func(st *planbrief.PlanStoryState) { st.EffectHold = "uncertain" },
		"effect holds":   func(st *planbrief.PlanStoryState) { st.EffectHolds = map[string]string{"r": "uncertain"} },
		"closed":         func(st *planbrief.PlanStoryState) { st.Lifecycle.State = "closed" },
		"retired":        func(st *planbrief.PlanStoryState) { st.Retired = true },
		"failed effect":  func(st *planbrief.PlanStoryState) { st.Effect = "failed" },
	}
	for name, hold := range holds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			held := cloneWorkflow(t, s)
			hold(&held.Stories[0])
			_, err := held.ReviewRevision("story-002", "story")
			if err == nil || errors.Is(err, planbrief.ErrPlanUpstreamPublishing) || err.Error() != "plan dependency story-001 is inactive or unresolved" {
				t.Fatalf("held upstream error = %v", err)
			}
		})
	}
}

// technicalOnlyEdit returns the story's draft with only its technical section changed.
func technicalOnlyEdit(t *testing.T, d planbrief.PlanDraft) planbrief.PlanDraft {
	t.Helper()
	raw, _ := json.Marshal(d.Architecture)
	var arch planbrief.PlanArchitecture
	if err := json.Unmarshal(raw, &arch); err != nil {
		t.Fatal(err)
	}
	d.Architecture = &arch
	d.Architecture.General[0].Decision = "owner revised the contract"
	return d
}

func TestPlanWorkflow_PutCandidateNeverMovesPastAnUnapprovedStage(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "story-003")
	if err := s.OpenPlanWait("story-003", "wait-story", "review", 100); err != nil {
		t.Fatal(err)
	}
	edit := technicalOnlyEdit(t, s.Stories[0].Draft)
	if edit.StageRevision("story") != s.Stories[0].Draft.StageRevision("story") {
		t.Fatal("fixture edit changed the story stage")
	}
	if err := s.PutCandidate(edit, "technical", "owner-edit-x"); err != nil {
		t.Fatal(err)
	}
	item := s.Stories[0]
	if item.Stage != "story" || item.Candidate != "owner-edit-x" || item.Wait != nil || len(item.Approvals) != 0 {
		t.Fatalf("owner technical edit moved the story: %+v", item)
	}
	if stage, _, ok := s.StoryNextStep("story-003"); stage != "story" || !ok {
		t.Fatalf("next step after the edit = (%q, %v), want story", stage, ok)
	}
	r := workflowReply(t, &s, 0, 30)
	if r.Stage != "story" || r.Body != "/approve-plan story-003 story owner-edit-x" {
		t.Fatalf("review after the edit = %+v", r)
	}
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("story approval after the edit: %v %v", ok, err)
	}

	// An approved story still returns to a changed earlier stage.
	approved := workflowFixture(t, "a")
	approveWorkflowStory(t, &approved, 0, 40)
	if err := approved.PutCandidate(technicalOnlyEdit(t, approved.Stories[0].Draft), "technical", "owner-edit-y"); err != nil {
		t.Fatal(err)
	}
	if approved.Stories[0].Stage != "technical" || approved.Stories[0].Approvals["story"] == "" {
		t.Fatalf("approved story did not return to technical: %+v", approved.Stories[0])
	}
}

// staleWaitFixture saves the #1155 shape: story-002 waits at technical while
// its story approval no longer matches its story revision.
func staleWaitFixture(t *testing.T) planbrief.PlanState {
	t.Helper()
	s := dependantFixture(t)
	r := workflowReply(t, &s, 1, 20)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve story-002 story: %v %v", ok, err)
	}
	r = workflowReply(t, &s, 0, 21)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve story-001 technical: %v %v", ok, err)
	}
	if err := s.OpenPlanWait("story-002", "wait-technical", "review", 101); err != nil {
		t.Fatal(err)
	}
	if s.Stories[1].Wait == nil || s.Stories[1].Wait.Stage != "technical" {
		t.Fatalf("fixture wait not at technical: %+v", s.Stories[1])
	}
	s.Stories[1].Approvals["story"] = "stale-story-approval"
	return s
}

func TestPlanWorkflow_NoWaitBehindAStaleEarlierApproval(t *testing.T) {
	t.Parallel()
	t.Run("repair clears the wait only", func(t *testing.T) {
		t.Parallel()
		s := staleWaitFixture(t)
		before := cloneWorkflow(t, s)
		if !s.ClearStaleWaits() {
			t.Fatal("stale wait was not cleared")
		}
		item, prior := s.Stories[1], before.Stories[1]
		if item.Wait != nil || item.Stage != prior.Stage || item.Candidate != prior.Candidate || !reflect.DeepEqual(item.Approvals, prior.Approvals) {
			t.Fatalf("repair changed more than the wait: %+v", item)
		}
		if !reflect.DeepEqual(s.Stories[0], before.Stories[0]) {
			t.Fatal("repair changed the upstream story")
		}
		if stage, _, ok := s.StoryNextStep("story-002"); stage != "story" || !ok {
			t.Fatalf("next step after repair = (%q, %v), want story", stage, ok)
		}
		if s.ClearStaleWaits() {
			t.Fatal("repair is not idempotent")
		}
	})
	t.Run("no wait is opened", func(t *testing.T) {
		t.Parallel()
		s := staleWaitFixture(t)
		s.Stories[1].Wait = nil
		for _, kind := range []string{"review", "question"} {
			if err := s.OpenPlanWait("story-002", "wait-"+kind, kind, 101); err != nil {
				t.Fatal(err)
			}
			if s.Stories[1].Wait != nil {
				t.Fatalf("%s wait opened behind a stale approval: %+v", kind, s.Stories[1].Wait)
			}
		}
		if stage, _, ok := s.StoryNextStep("story-002"); stage != "story" || !ok {
			t.Fatalf("next step = (%q, %v), want story", stage, ok)
		}
	})
	t.Run("healthy waits stay", func(t *testing.T) {
		t.Parallel()
		s := staleWaitFixture(t)
		s.Stories[1].Approvals["story"], _ = s.ReviewRevision("story-002", "story")
		if s.ClearStaleWaits() || s.Stories[1].Wait == nil {
			t.Fatal("a current wait was cleared")
		}
	})
	for name, hold := range map[string]func(*planbrief.PlanStoryState){
		"upstream publishing": func(st *planbrief.PlanStoryState) { st.Effect, st.EffectRequest = "pending", "plan-request-9-9" },
		"upstream held":       func(st *planbrief.PlanStoryState) { st.EffectHold = "uncertain" },
	} {
		t.Run(name+" leaves the wait untouched", func(t *testing.T) {
			t.Parallel()
			s := staleWaitFixture(t)
			s.Stories[1].Approvals["story"], _ = s.ReviewRevision("story-002", "story")
			wait := *s.Stories[1].Wait
			hold(&s.Stories[0])
			// Uncomputable revisions are no evidence: even a mismatching approval stays.
			s.Stories[1].Approvals["story"] = "unknowable-now"
			if s.ClearStaleWaits() || s.Stories[1].Wait == nil || *s.Stories[1].Wait != wait {
				t.Fatalf("wait changed while the upstream revision could not be computed: %+v", s.Stories[1].Wait)
			}
		})
	}
}

func TestPlanWorkflow_EarlierStageRefusalNamesTheWorkingCommand(t *testing.T) {
	t.Parallel()
	s := staleWaitFixture(t)
	// The read repair normally clears this wait first; the refusal must still
	// say what works now if such a state is ever read unrepaired.
	wait := s.Stories[1].Wait
	got := s.PlanReplyReason("story-002", "/approve-plan story-002 technical "+wait.Candidate)
	if !strings.HasPrefix(got, "An earlier stage of this story needs your approval again before this stage can be approved.") || !strings.Contains(got, "its next review will show the command to use.") || strings.Contains(got, "/approve-plan story-002 technical") {
		t.Fatalf("refusal names no way forward, or the refused command: %q", got)
	}
	if s.Stories[1].Wait == nil {
		t.Fatal("explaining a reply changed the state")
	}
	s.ClearStaleWaits()
	got = s.PlanReplyReason("story-002", "/approve-plan story-002 technical "+wait.Candidate)
	if !strings.Contains(got, "Nothing on this story is waiting for your approval right now; its next review will show the command to use.") {
		t.Fatalf("repaired reply explanation = %q", got)
	}
}

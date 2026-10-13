package planbrief_test

import (
	"encoding/json"
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
		{"question cannot promote", func(r *planbrief.PlanOwnerReply) {}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b")
			r := workflowReply(t, &s, 0, 11)
			tt.change(&r)
			if tt.question {
				s.Stories[0].Wait.Kind = "question"
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

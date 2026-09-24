package planbrief_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestPlanLifecycle_ClosureReasonsAndMembership(t *testing.T) {
	for _, reason := range []string{"completed", "not_planned", ""} {
		t.Run(reason+"/adopts closure without retirement", func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b")
			approveWorkflowStory(t, &s, 0, 10)
			before := s.PlanSetRevision()
			changed, err := s.ObserveLifecycle("a", planbrief.PlanLifecycle{State: "closed", Reason: reason, Revision: "remote-2"}, "")
			if err != nil || !changed {
				t.Fatalf("observation: changed=%v err=%v", changed, err)
			}
			a := s.Stories[0]
			if a.Active() || a.Retired || a.Lifecycle.Reason != reason || len(a.Approvals) != 0 {
				t.Fatalf("closure fabricated retirement/approval or lost reason: %+v", a)
			}
			if slices.Contains(s.EligiblePlanStories(), "a") || !slices.Contains(s.EligiblePlanStories(), "b") {
				t.Fatalf("wrong eligible set: %v", s.EligiblePlanStories())
			}
			if s.PlanSetRevision() == before {
				t.Fatal("active membership change retained old assembly revision")
			}
			after := cloneWorkflow(t, s)
			changed, err = s.ObserveLifecycle("a", a.Lifecycle, "")
			if err != nil || changed || !reflect.DeepEqual(s, after) {
				t.Fatal("unchanged poll changed retained state")
			}
			s.Stories[0].Draft.UserStory.Want = "inactive content edit"
			if s.PlanSetRevision() != after.PlanSetRevision() {
				t.Fatal("inactive draft leaked into active-set hash")
			}
		})
	}
}

func TestPlanLifecycle_DependencyAndApprovalInvalidation(t *testing.T) {
	for _, hold := range []string{"", "identity mismatch", "reparented", "child unavailable"} {
		t.Run(hold+"/isolates dependent closure", func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b", "c", "independent")
			s.Stories[1].Draft.Dependencies.StoryKeys = []string{"a"}
			s.Stories[2].Draft.Dependencies.StoryKeys = []string{"b"}
			for i := range s.Stories {
				approveWorkflowStory(t, &s, i, int64(10+i*10))
			}
			before := cloneWorkflow(t, s)
			s.AssembledApproval, s.AssembledWait = "old", "old"
			observation := planbrief.PlanLifecycle{State: "closed", Reason: "completed"}
			if hold != "" {
				observation = planbrief.PlanLifecycle{}
			}
			if _, err := s.ObserveLifecycle("a", observation, hold); err != nil {
				t.Fatal(err)
			}
			for _, i := range []int{0, 1, 2} {
				if len(s.Stories[i].Approvals) != 0 {
					t.Fatalf("story %s retained stale approval", s.Stories[i].Key)
				}
			}
			if !reflect.DeepEqual(s.Stories[3], before.Stories[3]) {
				t.Fatal("independent approvals changed")
			}
			if s.AssembledApproval != "" || s.AssembledWait != "" {
				t.Fatal("assembly retained stale review")
			}
			eligible := s.EligiblePlanStories()
			if slices.Contains(eligible, "a") || slices.Contains(eligible, "b") || slices.Contains(eligible, "c") {
				t.Fatalf("unavailable dependency selected: %v", eligible)
			}
			if hold != "" && s.Stories[0].Lifecycle.State != "" {
				t.Fatal("unverified child inferred closed")
			}
		})
	}
}

func TestPlanLifecycle_ReopenReevaluatesApproval(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a", "b")
	approveWorkflowStory(t, &s, 0, 10)
	if _, err := s.ObserveLifecycle("a", planbrief.PlanLifecycle{State: "closed", Reason: "completed"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ObserveLifecycle("a", planbrief.PlanLifecycle{State: "open", Revision: "new"}, ""); err != nil {
		t.Fatal(err)
	}
	a := s.Stories[0]
	if !a.Active() || !a.Lifecycle.Reopened || len(a.Approvals) != 0 || a.Stage != "story" {
		t.Fatalf("reopen revived stale approval: %+v", a)
	}
	if !slices.Contains(s.EligiblePlanStories(), "a") {
		t.Fatal("verified reopened story cannot reevaluate")
	}
	s.Stories[0].Retired = true
	if s.Stories[0].Active() {
		t.Fatal("reopen revived retired scope")
	}
}

func TestPlanLifecycle_UnverifiedObservationPreservesFactsAndDebt(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a")
	s.Stories[0].Lifecycle = planbrief.PlanLifecycle{State: "closed", Reason: "completed", Revision: "known"}
	s.Stories[0].Hold = "unknown remote request"
	s.Stories[0].Effect = "uncertain"
	before := s.Stories[0].Lifecycle
	if _, err := s.ObserveLifecycle("a", planbrief.PlanLifecycle{}, "identity mismatch"); err != nil {
		t.Fatal(err)
	}
	a := s.Stories[0]
	if a.Lifecycle != before || a.Hold != "unknown remote request" || a.Effect != "uncertain" || !a.Blocked() {
		t.Fatalf("observation discarded independent facts/debt: %+v", a)
	}
	if _, err := s.ObserveLifecycle("a", before, ""); err != nil {
		t.Fatal(err)
	}
	if !s.Stories[0].Blocked() || s.Stories[0].LifecycleHold != "" {
		t.Fatal("resolved discovery cleared unresolved effect")
	}
}

func TestPlanLifecycle_ReadinessRemainingSet(t *testing.T) {
	tests := []struct {
		name   string
		change func(*planbrief.PlanState)
		want   bool
	}{
		{"resolved closure/remaining set ready", func(*planbrief.PlanState) {}, true},
		{"inactive uncertain effect/not ready", func(s *planbrief.PlanState) { s.Stories[0].Effect = "uncertain" }, false},
		{"inactive pending effect/not ready", func(s *planbrief.PlanState) { s.Stories[0].Effect = "pending" }, false},
		{"inactive lifecycle hold/not ready", func(s *planbrief.PlanState) { s.Stories[0].LifecycleHold = "identity mismatch" }, false},
		{"inactive generic hold/not ready", func(s *planbrief.PlanState) { s.Stories[0].Hold = "unknown effect" }, false},
		{"closed dependency/not ready", func(s *planbrief.PlanState) { s.Stories[1].Draft.Dependencies.StoryKeys = []string{"a"} }, false},
		{"empty active set/not ready", func(s *planbrief.PlanState) { s.Stories[1].Lifecycle.State = "closed" }, false},
		{"stale assembly/not ready", func(s *planbrief.PlanState) { s.AssembledApproval = "old" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b")
			approveWorkflowStory(t, &s, 1, 20)
			if _, err := s.ObserveLifecycle("a", planbrief.PlanLifecycle{State: "closed", Reason: "completed"}, ""); err != nil {
				t.Fatal(err)
			}
			scope := planbrief.ContentScope{Roles: []string{"engineer"}}
			ref, err := s.OpenAssembledReview(scope)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.ApproveAssembled(7, 7, 90, "/approve-plan-set "+ref, scope); err != nil {
				t.Fatal(err)
			}
			s.PublishedRevision, s.SavedRevision = ref, ref
			tt.change(&s)
			if got := s.PlanReady(scope); got != tt.want {
				t.Fatalf("PlanReady=%v want %v", got, tt.want)
			}
		})
	}
}

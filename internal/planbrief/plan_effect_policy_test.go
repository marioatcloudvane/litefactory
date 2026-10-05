package planbrief_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestPlanWorkflow_EffectDebtBlocksTransitiveDependents(t *testing.T) {
	for _, operation := range []string{"create", "attach", "update", "retire"} {
		t.Run(operation+"/independent sibling remains eligible", func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b", "c", "healthy", "approved")
			s.Stories[1].Draft.Dependencies.StoryKeys = []string{"a"}
			s.Stories[2].Draft.Dependencies.StoryKeys = []string{"b"}
			approveWorkflowStory(t, &s, 4, 100)
			approved := cloneWorkflow(t, s).Stories[4]
			s.Stories[0].Effect = operation + "-original-request"
			s.Stories[0].EffectHold = "remote outcome unknown"

			got := s.EligiblePlanStories()

			if !reflect.DeepEqual(got, []string{"healthy"}) {
				t.Fatalf("eligible = %v, want only independent healthy sibling", got)
			}
			for _, key := range []string{"a", "b", "c"} {
				if _, err := s.ReviewRevision(key, "story"); err == nil {
					t.Errorf("unresolved upstream effect did not block %s", key)
				}
			}
			if !reflect.DeepEqual(s.Stories[4], approved) {
				t.Fatal("unrelated approved story changed")
			}
			s.Stories[0].Effect = "applied"
			s.Stories[0].EffectHold = ""
			// b and c depend on a and b, which have not approved the story stage yet,
			// so they wait; only the stories with no unapproved upstream are eligible.
			if got = s.EligiblePlanStories(); !reflect.DeepEqual(got, []string{"a", "healthy"}) {
				t.Fatalf("after effect settlement eligible = %v", got)
			}
		})
	}
}

func TestPlanWorkflow_LifecycleDoesNotSettleUnknownEffect(t *testing.T) {
	for _, state := range []string{"open", "closed"} {
		t.Run(state+"/retains original request and separate causes", func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "healthy")
			s.Stories[0].Effect = "original-request"
			s.Stories[0].EffectHold = "retirement response lost"
			s.Stories[0].Hold = "content needs clarification"
			s.Stories[0].LifecycleHold = "prior observation unavailable"

			changed, err := s.ObserveLifecycle("a", planbrief.PlanLifecycle{State: state, Reason: "", Revision: "verified-revision"}, "")

			if err != nil || !changed {
				t.Fatalf("observe = %v, %v", changed, err)
			}
			got := s.Stories[0]
			if got.Effect != "original-request" || got.EffectHold != "retirement response lost" || got.Hold != "content needs clarification" || got.LifecycleHold != "" {
				t.Fatalf("observation lost or conflated retained causes: %+v", got)
			}
			if !got.Blocked() || got.Retired || got.Lifecycle.Reason != "" {
				t.Fatalf("observation manufactured settlement or retirement: %+v", got)
			}
			if eligible := s.EligiblePlanStories(); !reflect.DeepEqual(eligible, []string{"healthy"}) {
				t.Fatalf("eligible = %v", eligible)
			}
		})
	}
}

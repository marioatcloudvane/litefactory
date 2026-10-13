package daemon

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// approvedPlanState holds one story with the given task owner and, when
// approve is set, the owner's approval of all three stages.
func approvedPlanState(t *testing.T, owner string, approve bool) planbrief.PlanState {
	t.Helper()
	d := planbrief.PlanDraft{Key: "story-a", Name: "Shape a story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "shared understanding"}, Problem: "Unclear requirements", InScope: []string{"Story conversation"}, NotInScope: []string{"Implementation"},
		Architecture: &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Ownership", Decision: "Separate roles", Rationale: "Focused responsibilities"}}}, Guidance: []string{"agent-setup"},
		Criteria: []planbrief.PlanCriterion{{ID: "AC-001", Text: "Draft retains intent", ProofMethod: "Unit test"}},
		Tasks:    []planbrief.TaskContent{{ID: "T-001", Description: "Implement draft", OwnerRole: owner, CriterionIDs: []string{"AC-001"}}}}
	s := planbrief.PlanState{SharedRevision: "shared-1"}
	if err := s.PutCandidate(d, "story", "candidate"); err != nil {
		t.Fatal(err)
	}
	if !approve {
		return s
	}
	for n := range 3 {
		st := s.Stories[0]
		if err := s.OpenPlanWait(st.Key, "wait-"+st.Stage, "review", 100); err != nil {
			t.Fatal(err)
		}
		r := planbrief.PlanOwnerReply{ID: int64(10 + n), OwnerID: 7, ActorID: 7, IssueID: 100, Key: st.Key, Stage: st.Stage, Candidate: st.Candidate, WaitID: "wait-" + st.Stage, Body: "/approve-plan " + st.Key + " " + st.Stage + " " + st.Candidate}
		if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
			t.Fatalf("approve %s: %v %v", r.Stage, ok, err)
		}
	}
	return s
}

func TestAssemblyBlocked(t *testing.T) {
	scope := planbrief.ReleaseScope([]string{"implementation-engineer"}, []string{"agent-setup"})
	t.Run("approved but invalid is reported", func(t *testing.T) {
		s := approvedPlanState(t, "frontend-engineer", true)
		if _, due := parentWorkIDWithScope(s, scope); due {
			t.Fatal("assembly due for an unknown owner")
		}
		if err := assemblyBlocked(s, scope); err == nil || !strings.Contains(err.Error(), "authorized role") {
			t.Fatal("blocked assembly not explained:", err)
		}
	})
	t.Run("awaiting approval is silent", func(t *testing.T) {
		if err := assemblyBlocked(approvedPlanState(t, "frontend-engineer", false), scope); err != nil {
			t.Fatal("waiting plan reported as blocked:", err)
		}
	})
	t.Run("go- owner of a release role assembles", func(t *testing.T) {
		s := approvedPlanState(t, "go-implementation-engineer", true)
		if _, due := parentWorkIDWithScope(s, scope); !due {
			t.Fatal("assembly not due")
		}
		if err := assemblyBlocked(s, scope); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("empty plan is silent", func(t *testing.T) {
		if err := assemblyBlocked(planbrief.PlanState{}, scope); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStoryWorkIDRetainedEffectHolds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		story planbrief.PlanStoryState
		want  bool
	}{
		{"request hold", planbrief.PlanStoryState{Effect: "uncertain", EffectRequest: "original", EffectHolds: map[string]string{"original": "content conflict"}}, false},
		{"legacy hold", planbrief.PlanStoryState{Effect: "uncertain", EffectRequest: "original", EffectHold: "content conflict"}, false},
		{"independent hold", planbrief.PlanStoryState{Effect: "pending", EffectRequest: "newer", EffectHolds: map[string]string{"original": "content conflict"}}, false},
		{"unheld uncertain", planbrief.PlanStoryState{Effect: "uncertain", EffectRequest: "original"}, true},
		{"unheld pending", planbrief.PlanStoryState{Effect: "pending", EffectRequest: "original"}, true},
		{"ordinary hold with reconciliation debt", planbrief.PlanStoryState{Effect: "pending", EffectRequest: "original", Hold: "waiting for owner"}, true},
		{"owner wait", planbrief.PlanStoryState{Hold: "waiting for owner"}, false},
		{"upstream wait", planbrief.PlanStoryState{Draft: planbrief.PlanDraft{Dependencies: planbrief.DependencyContent{StoryKeys: []string{"upstream"}}}}, false},
		{"healthy draft", planbrief.PlanStoryState{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.story.Key = "story-001"
			state := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Promoted: true}, Stories: []planbrief.PlanStoryState{tc.story}}
			id, ok := storyWorkID(state, tc.story.Key)
			if ok != tc.want {
				t.Fatalf("work=%d eligible=%v want %v", id, ok, tc.want)
			}
			if ok && tc.story.Effect != "" && id != planWorkID("effect", tc.story.Key, tc.story.EffectRequest) {
				t.Fatal("reconciliation identity changed")
			}
		})
	}
	story := planbrief.PlanStoryState{Key: "story-001", Effect: "uncertain"}
	story.SetEffectHold("original", "content conflict")
	story.ClearEffectHold("original")
	story.Effect = "applied"
	state := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Promoted: true}, Stories: []planbrief.PlanStoryState{story}}
	if _, ok := storyWorkID(state, story.Key); !ok {
		t.Fatal("legitimately cleared hold blocked draft")
	}
}

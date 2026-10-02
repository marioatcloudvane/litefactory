package turn

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestValidateRetirementReview(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*planbrief.PlanState, *PlanStoryAuthority)
		wantHold bool
	}{
		{name: "unchanged retained review/allows reconciliation"},
		{name: "own closed lifecycle/allows reconciliation", change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) {
			s.Stories[0].Lifecycle = planbrief.PlanLifecycle{State: "closed", Reason: "not_planned"}
			s.Stories[0].Effect = "original-request"
			s.Stories[0].EffectHold = "response lost"
		}},
		{name: "missing authenticated confirmation/holds", wantHold: true, change: func(_ *planbrief.PlanState, a *PlanStoryAuthority) { a.Confirmation = nil }},
		{name: "missing original review/holds", wantHold: true, change: func(_ *planbrief.PlanState, a *PlanStoryAuthority) { a.ReviewRevision = "" }},
		{name: "later reopened child/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) { s.Stories[0].Lifecycle.Reopened = true }},
		{name: "unverified child relationship/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) {
			s.Stories[0].LifecycleHold = "parent identity changed"
		}},
		{name: "newer candidate/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) { s.Stories[0].Candidate = "new-candidate" }},
		{name: "newer shared decision/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) { s.SharedRevision = "shared-new" }},
		{name: "newer target content/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) {
			s.Stories[0].Draft.UserStory.Want = "new requirement"
		}},
		{name: "transitive upstream effect unresolved/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) {
			s.Stories[2].EffectHold = "create outcome unknown"
		}},
		{name: "transitive upstream closed/holds", wantHold: true, change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) { s.Stories[2].Lifecycle.State = "closed" }},
		{name: "unrelated effect unresolved/allows reconciliation", change: func(s *planbrief.PlanState, _ *PlanStoryAuthority) {
			s.Stories[3].EffectHold = "create outcome unknown"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := planbrief.PlanState{SharedRevision: "shared-original"}
			for _, key := range []string{"target", "dependency", "transitive", "independent"} {
				s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: key, Candidate: "candidate-original", Stage: "technical", Draft: planbrief.PlanDraft{Key: key}})
			}
			s.Stories[0].Draft.Dependencies.StoryKeys = []string{"dependency"}
			s.Stories[1].Draft.Dependencies.StoryKeys = []string{"transitive"}
			revision, err := s.ReviewRevision("target", "technical")
			if err != nil {
				t.Fatal(err)
			}
			a := PlanStoryAuthority{RetirementCandidate: "candidate-original", ReviewRevision: revision, Confirmation: &planbrief.PlanOwnerReply{ID: 10, ActorID: 7, OwnerID: 7}}
			r := graphexec.PlanStoryRequest{RequestID: "original-request", Operation: "retire", Stage: "technical"}
			d := store.PlanStoryDraft{Key: "target"}
			if tt.change != nil {
				tt.change(&s, &a)
			}
			before, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}

			err = validateRetirementReview(s, a, r, d)

			if tt.wantHold && !errors.Is(err, ErrPlanStoryHeld) {
				t.Fatalf("error = %v, want story-local hold", err)
			}
			if !tt.wantHold && err != nil {
				t.Fatalf("valid original review refused: %v", err)
			}
			after, marshalErr := json.Marshal(s)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if string(before) != string(after) {
				t.Fatal("review check changed retained lifecycle, debt or approval state")
			}
		})
	}
}

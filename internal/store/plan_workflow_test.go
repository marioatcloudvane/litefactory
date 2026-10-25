package store

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// staleWaitState saves story-002 (depending on story-001) waiting at technical.
// Its story approval is current unless stale is set.
func staleWaitState(t *testing.T, stale bool) planbrief.PlanState {
	t.Helper()
	s := planbrief.PlanState{SharedRevision: "shared-1", Stories: []planbrief.PlanStoryState{
		{Key: "story-001", Stage: "technical", Candidate: "c1", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Upstream"}, Approvals: map[string]string{}, Effect: "applied"},
		{Key: "story-002", Stage: "technical", Candidate: "c2", Draft: planbrief.PlanDraft{Key: "story-002", Name: "Dependant", Dependencies: planbrief.DependencyContent{StoryKeys: []string{"story-001"}}}, Approvals: map[string]string{}, Effect: "applied"},
	}}
	for _, key := range []string{"story-001", "story-002"} {
		ref, err := s.ReviewRevision(key, "story")
		if err != nil {
			t.Fatal(err)
		}
		for i := range s.Stories {
			if s.Stories[i].Key == key {
				s.Stories[i].Approvals["story"] = ref
			}
		}
	}
	if ref, err := s.ReviewRevision("story-001", "technical"); err == nil {
		s.Stories[0].Approvals["technical"] = ref
	}
	ref, err := s.ReviewRevision("story-002", "technical")
	if err != nil {
		t.Fatal(err)
	}
	s.Stories[1].Wait = &planbrief.PlanWait{ID: "wait-technical", Stage: "technical", Candidate: "c2", Revision: ref, Kind: "review", IssueID: 102}
	if stale {
		s.Stories[1].Approvals["story"] = "stale-story-approval"
	}
	return s
}

func TestPlanWorkflowStore_ReadClearsAWaitBehindAStaleEarlierApproval(t *testing.T) {
	ctx := t.Context()
	db := newTestDB(t)
	session := testSession(t, db, 1160)
	if _, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, staleWaitState(t, true)); err != nil {
		t.Fatal(err)
	}
	state, _, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	item := state.Stories[1]
	if item.Wait != nil || item.Stage != "technical" || item.Candidate != "c2" || item.Approvals["story"] != "stale-story-approval" {
		t.Fatalf("read did not clear only the stale wait: %+v", item)
	}
	if stage, _, ok := state.StoryNextStep("story-002"); stage != "story" || !ok {
		t.Fatalf("next step after the read = (%q, %v), want story", stage, ok)
	}
	// The next write of any story saves the cleared wait.
	if err = db.UpdatePlanWorkflowState(ctx, session.TaskRef, func(s *planbrief.PlanState) (bool, error) {
		s.ConsumedReplies = append(s.ConsumedReplies, 9)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = db.sql.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, session.TaskRef).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "wait-technical") {
		t.Fatalf("written state kept the stale wait: %s", raw)
	}
}

func TestPlanWorkflowStore_UncomputableRevisionLeavesWaitsUntouched(t *testing.T) {
	cases := map[string]func(*planbrief.PlanStoryState){
		"publishing": func(st *planbrief.PlanStoryState) { st.Effect, st.EffectRequest = "pending", "plan-request-7-8" },
		"held":       func(st *planbrief.PlanStoryState) { st.EffectHold = "uncertain" },
	}
	for name, hold := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			db := newTestDB(t)
			session := testSession(t, db, 1161)
			state := staleWaitState(t, false)
			want := *state.Stories[1].Wait
			hold(&state.Stories[0])
			if _, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
				t.Fatal(err)
			}
			read, _, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			if read.Stories[1].Wait == nil || *read.Stories[1].Wait != want {
				t.Fatalf("read changed the wait while the upstream revision cannot be computed: %+v", read.Stories[1].Wait)
			}
			if err = db.UpdatePlanWorkflowState(ctx, session.TaskRef, func(s *planbrief.PlanState) (bool, error) {
				s.ConsumedReplies = append(s.ConsumedReplies, 9)
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			// The upstream returns to applied with unchanged content: the wait
			// was kept through the write and is still approvable.
			if err = db.UpdatePlanWorkflowState(ctx, session.TaskRef, func(s *planbrief.PlanState) (bool, error) {
				s.Stories[0].Effect, s.Stories[0].EffectHold = "applied", ""
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			read, _, err = db.GetPlanWorkflowState(ctx, session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			if read.Stories[1].Wait == nil || *read.Stories[1].Wait != want {
				t.Fatalf("a write in the window changed the wait: %+v", read.Stories[1].Wait)
			}
			if command := read.PlanCurrentCommand("story-002"); command != "/approve-plan story-002 technical c2" {
				t.Fatalf("wait no longer approvable: %q", command)
			}
			if ok, err := read.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: 50, OwnerID: 7, ActorID: 7, IssueID: 102, Key: "story-002", Stage: "technical", Candidate: "c2", WaitID: "wait-technical", Body: "/approve-plan story-002 technical c2"}); err != nil || !ok {
				t.Fatalf("approval after the window: %v %v", ok, err)
			}
		})
	}
}

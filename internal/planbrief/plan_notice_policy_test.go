package planbrief_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestPlanNoticeState_MaterialChanges(t *testing.T) {
	tests := []struct {
		name       string
		change     func(*planbrief.PlanStoryState)
		wantChange bool
	}{
		{"poll revision/no new notice", func(s *planbrief.PlanStoryState) { s.Lifecycle.Revision = "next-poll" }, false},
		{"candidate churn/no new notice", func(s *planbrief.PlanStoryState) { s.Candidate = "next-candidate" }, false},
		{"closure/new notice", func(s *planbrief.PlanStoryState) { s.Lifecycle.State = "closed" }, true},
		{"actual reason/new notice", func(s *planbrief.PlanStoryState) { s.Lifecycle.State = "closed"; s.Lifecycle.Reason = "completed" }, true},
		{"verified reopening/new notice", func(s *planbrief.PlanStoryState) { s.Lifecycle.Reopened = true }, true},
		{"lifecycle hold/new notice", func(s *planbrief.PlanStoryState) { s.LifecycleHold = "parent changed" }, true},
		{"plan hold/new notice", func(s *planbrief.PlanStoryState) { s.Hold = "dependency unavailable" }, true},
		{"request cause/new notice", func(s *planbrief.PlanStoryState) { s.SetEffectHold("request-a", "original outcome unknown") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := planbrief.PlanStoryState{Key: "a", Lifecycle: planbrief.PlanLifecycle{State: "open", Revision: "first"}}
			before := s.NoticeState()
			tt.change(&s)
			after := s.NoticeState()
			if changed := !reflect.DeepEqual(before, after); changed != tt.wantChange {
				t.Fatalf("material change = %v, want %v", changed, tt.wantChange)
			}
			if before.Significant() {
				t.Fatal("initial healthy observation produces a notice")
			}
			if tt.wantChange && !after.Significant() {
				t.Fatal("material hold or lifecycle event was hidden")
			}
		})
	}
}

func TestPlanEffectHolds_PreserveIndependentRequests(t *testing.T) {
	t.Parallel()
	s := planbrief.PlanStoryState{Key: "a", Effect: "uncertain", EffectRequest: "legacy-z", EffectHold: "legacy outcome unknown"}
	s.SetEffectHold("request-b", "attach outcome unknown")
	s.SetEffectHold("request-a", "create outcome unknown")
	want := map[string]string{"legacy-z": "legacy outcome unknown", "request-a": "create outcome unknown", "request-b": "attach outcome unknown"}
	if !reflect.DeepEqual(s.EffectHolds, want) || s.EffectRequest != "legacy-z" {
		t.Fatalf("retained holds = %+v representative = %q", s.EffectHolds, s.EffectRequest)
	}
	notice := s.NoticeState()
	notice.RequestHolds["request-a"] = "changed copy"
	if s.EffectHolds["request-a"] != want["request-a"] {
		t.Fatal("notice aliases retained request causes")
	}
	prior := s
	s.ClearEffectHold("legacy-z")
	if s.EffectRequest != "request-a" || s.EffectHold != "create outcome unknown" || len(s.EffectHolds) != 2 || !s.Blocked() {
		t.Fatalf("clearing one request lost sibling debt: %+v", s)
	}
	if len(prior.EffectHolds) != 3 {
		t.Fatal("clear mutated earlier state snapshot")
	}
	s.ClearEffectHold("never-recorded")
	if len(s.EffectHolds) != 2 {
		t.Fatal("clearing another request discarded retained outcomes")
	}
	s.ClearEffectHold("request-a")
	if s.EffectRequest != "request-b" || s.EffectHold != "attach outcome unknown" {
		t.Fatalf("remaining original request missing: %+v", s)
	}
	s.ClearEffectHold("request-b")
	if len(s.EffectHolds) != 0 || s.EffectHold != "" {
		t.Fatalf("settled request causes retained: %+v", s)
	}
}

func TestPlanNoticeState_StableAcrossRequestOrderAndRestart(t *testing.T) {
	t.Parallel()
	first := planbrief.PlanStoryState{Key: "a"}
	second := planbrief.PlanStoryState{Key: "a"}
	for _, key := range []string{"b", "a"} {
		first.SetEffectHold(key, "unknown "+key)
	}
	for _, key := range []string{"a", "b"} {
		second.SetEffectHold(key, "unknown "+key)
	}
	one, err := json.Marshal(first.NoticeState())
	if err != nil {
		t.Fatal(err)
	}
	two, err := json.Marshal(second.NoticeState())
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatalf("same material facts differ by request order: %s / %s", one, two)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var restored planbrief.PlanStoryState
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.NoticeState(), restored.NoticeState()) {
		t.Fatal("serialized restart changed notice facts")
	}
}

func TestPlanReplyReason_CurrentState(t *testing.T) {
	tests := []struct {
		name, body string
		change     func(*planbrief.PlanState)
		wantHold   bool
	}{
		{name: "current healthy approval/allowed", body: "/approve-plan a story a-candidate"},
		{name: "healthy discussion/allowed", body: "Please explain the database choice."},
		{name: "independent held sibling/allowed", body: "/approve-plan a story a-candidate", change: func(s *planbrief.PlanState) { s.Stories[1].SetEffectHold("b-request", "unknown result") }},
		{name: "old candidate/explained", body: "/approve-plan a story old-candidate", wantHold: true},
		{name: "closed story/explained", body: "/approve-plan a story a-candidate", wantHold: true, change: func(s *planbrief.PlanState) { s.Stories[0].Lifecycle.State = "closed" }},
		{name: "own unknown request/explained", body: "/approve-plan a story a-candidate", wantHold: true, change: func(s *planbrief.PlanState) { s.Stories[0].SetEffectHold("a-request", "unknown result") }},
		{name: "changed shared review/explained", body: "/approve-plan a story a-candidate", wantHold: true, change: func(s *planbrief.PlanState) { s.SharedRevision = "new" }},
		{name: "missing current prompt/explained", body: "/approve-plan a story a-candidate", wantHold: true, change: func(s *planbrief.PlanState) { s.Stories[0].Wait = nil }},
		{name: "open question/explained", body: "/approve-plan a story a-candidate", wantHold: true, change: func(s *planbrief.PlanState) { s.Stories[0].Draft.Questions = []string{"Which owner?"} }},
		{name: "retirement against approval prompt/explained", body: "/retire-plan a a-candidate", wantHold: true},
		{name: "inactive dependency/explained", body: "/approve-plan a story a-candidate", wantHold: true, change: func(s *planbrief.PlanState) {
			s.Stories[0].Draft.Dependencies.StoryKeys = []string{"b"}
			s.Stories[1].Lifecycle.State = "closed"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, "a", "b")
			_ = workflowReply(t, &s, 0, 10)
			if tt.change != nil {
				tt.change(&s)
			}
			before := cloneWorkflow(t, s)
			reason := s.PlanReplyReason("a", tt.body)
			if (reason != "") != tt.wantHold {
				t.Fatalf("reply reason = %q, want explanation %v", reason, tt.wantHold)
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("reply disposition mutated approval, request or consumption state")
			}
		})
	}
}

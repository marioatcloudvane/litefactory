package planbrief

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPlanStorySetRevision_FollowsContentOnly(t *testing.T) {
	t.Parallel()
	base := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "Do A", DependsOn: []string{"c", "b"}}, {ID: "b", Name: "B", Intent: "Do B"}, {ID: "c", Name: "C", Intent: "Do C"}}
	rev := PlanStorySetRevision(base)
	if len(rev) != 64 || rev != PlanStorySetRevision(base) {
		t.Fatalf("revision %q is not a stable 64-character digest", rev)
	}
	same := []PlanStorySetEntry{{ID: " a", Name: "A ", Intent: "Do A", DependsOn: []string{"b", "c"}}, {ID: "b", Name: "B", Intent: "Do B", DependsOn: []string{}}, {ID: "c", Name: "C", Intent: "Do C"}}
	if PlanStorySetRevision(same) != rev {
		t.Fatal("whitespace, dependency order or an empty dependency list changed the revision")
	}
	for name, change := range map[string]func([]PlanStorySetEntry){
		"order":      func(e []PlanStorySetEntry) { e[1], e[2] = e[2], e[1] },
		"name":       func(e []PlanStorySetEntry) { e[0].Name = "Other" },
		"intent":     func(e []PlanStorySetEntry) { e[1].Intent = "Other" },
		"dependency": func(e []PlanStorySetEntry) { e[0].DependsOn = []string{"b"} },
		"id":         func(e []PlanStorySetEntry) { e[2].ID = "z" },
		"removed":    func(e []PlanStorySetEntry) { e[2] = PlanStorySetEntry{ID: "d", Name: "D", Intent: "Do D"} },
	} {
		changed := make([]PlanStorySetEntry, len(base))
		copy(changed, base)
		change(changed)
		if PlanStorySetRevision(changed) == rev {
			t.Fatalf("a changed %s kept the revision", name)
		}
	}
}

func TestProposeStorySet_RecordsReplacesAndRefuses(t *testing.T) {
	t.Parallel()
	first := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "Do A"}, {ID: "b", Name: "B", Intent: "Do B", DependsOn: []string{"a"}}}
	s := PlanState{SharedRevision: "initial"}
	rev, changed, err := s.ProposeStorySet(first)
	if err != nil || !changed || s.StorySet == nil || s.StorySet.Revision != rev || s.StorySet.Promoted || len(s.StorySet.Keys) != 0 {
		t.Fatalf("first proposal: %q %v %v %+v", rev, changed, err, s.StorySet)
	}
	// The saved set does not alias the proposal.
	first[1].DependsOn[0] = "z"
	if s.StorySet.Entries[1].DependsOn[0] != "a" {
		t.Fatal("the saved set shares its dependency list with the proposal")
	}
	first[1].DependsOn[0] = "a"
	if again, changed, err := s.ProposeStorySet(first); err != nil || changed || again != rev {
		t.Fatalf("same content: %q %v %v", again, changed, err)
	}
	second := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "Do A"}}
	replaced, changed, err := s.ProposeStorySet(second)
	if err != nil || !changed || replaced == rev || len(s.StorySet.Entries) != 1 {
		t.Fatalf("replacement: %q %v %v", replaced, changed, err)
	}
	s.StorySet.Promoted, s.StorySet.PromotedRevision = true, replaced
	if _, _, err = s.ProposeStorySet(first); !errors.Is(err, ErrPlanStorySetPromoted) {
		t.Fatalf("a promoted set was replaced: %v", err)
	}
	if again, changed, err := s.ProposeStorySet(second); err != nil || changed || again != replaced {
		t.Fatalf("the promoted set's own content: %q %v %v", again, changed, err)
	}
	withStories := PlanState{SharedRevision: "initial", Stories: []PlanStoryState{{Key: "story-001", Stage: "story"}}}
	if _, _, err = withStories.ProposeStorySet(first); !errors.Is(err, ErrPlanStorySetHasStories) || withStories.StorySet != nil {
		t.Fatalf("a scope with stories took a set: %v", err)
	}
	if _, _, err = (&PlanState{}).ProposeStorySet(nil); err == nil {
		t.Fatal("an empty set was recorded")
	}
}

// A Plan saved before story sets existed carries no story_set member, loads, and
// saves back without one.
func TestPlanState_SavedBeforeStorySetsStillLoads(t *testing.T) {
	t.Parallel()
	old := `{"shared_revision":"initial","stories":[{"key":"story-001","stage":"story","candidate":"c1","draft":{"key":"story-001"},"approvals":{},"retired":false}],"consumed_replies":[3]}`
	var s PlanState
	if err := json.Unmarshal([]byte(old), &s); err != nil {
		t.Fatalf("old state does not load: %v", err)
	}
	if s.StorySet != nil || s.StoryDrafted("story-001") != true {
		t.Fatalf("old state loaded wrongly: %+v", s)
	}
	raw, err := json.Marshal(s)
	if err != nil || strings.Contains(string(raw), "story_set") {
		t.Fatalf("a state with no set gained a story_set member: %s %v", raw, err)
	}
	proposed := PlanState{SharedRevision: "initial"}
	if _, _, err = proposed.ProposeStorySet([]PlanStorySetEntry{{ID: "a", Name: "A", Intent: "Do A"}}); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(proposed)
	if err != nil {
		t.Fatal(err)
	}
	var back PlanState
	if err = json.Unmarshal(raw, &back); err != nil || back.StorySet == nil || back.StorySet.Revision != proposed.StorySet.Revision {
		t.Fatalf("a proposed set does not round-trip: %s %v", raw, err)
	}
}

func TestRenderPlanStorySet_ShowsRevisionOrderDependenciesAndTheCommand(t *testing.T) {
	t.Parallel()
	s := PlanState{SharedRevision: "initial"}
	rev, _, err := s.ProposeStorySet([]PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Keep data | safely\nacross restarts", DependsOn: []string{"s2"}}, {ID: "s2", Name: "Schema", Intent: "Define tables"}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := RenderPlanStorySet(s.StorySet)
	if err != nil {
		t.Fatal(err)
	}
	// A pipe or line break in agent text stays inside its cell.
	want := "Proposed story set, revision `" + rev + "`\n\n" +
		"| ID | Name | Description | Dependencies |\n|---|---|---|---|\n" +
		"| `s1` | Storage | Keep data \\| safely across restarts | `s2` |\n" +
		"| `s2` | Schema | Define tables | none |\n"
	if !strings.HasPrefix(text, want) || !strings.Contains(text, "`/approve-plan set "+rev+"`") || PlanSetPromotionCommand(rev) != "/approve-plan set "+rev {
		t.Fatalf("rendering:\n%s", text)
	}
	if _, err = RenderPlanStorySet(nil); err == nil {
		t.Fatal("no set rendered")
	}
	broken := s.StorySet.Clone()
	broken.Entries[0].DependsOn = []string{"ghost"}
	if _, err = RenderPlanStorySet(broken); err == nil {
		t.Fatal("an unknown dependency rendered")
	}
}

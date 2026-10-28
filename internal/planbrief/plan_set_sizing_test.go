package planbrief

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPlanStorySetSizing_HistoricalUnsizedRevision(t *testing.T) {
	t.Parallel()
	// Fixed pre-sizing serialization, including historical member order and hash kind.
	const fixture = `{"kind":"plan-story-set/1","entries":[{"id":"a","name":"A","intent":"Do A","depends_on":["b","c"]},{"id":"b","name":"B","intent":"Do B"},{"id":"c","name":"C","intent":"Do C"}]}`
	const revision = "d2b17434620faa3b99a4b0ef2f6db49c05eec897382b9106efc9592a9a04a7a4"
	var old struct {
		Kind    string              `json:"kind"`
		Entries []PlanStorySetEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(fixture), &old); err != nil {
		t.Fatal(err)
	}
	if got := PlanStorySetRevision(old.Entries); got != revision {
		t.Fatalf("legacy revision changed: %s", got)
	}
	encoded, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != fixture {
		t.Fatalf("legacy entry bytes changed: %s", encoded)
	}
	s := PlanState{StorySet: &PlanStorySetState{Revision: revision, Entries: old.Entries}}
	if got, changed, err := s.ProposeStorySet(old.Entries); err != nil || changed || got != revision || s.StorySet.SizingApplicable() {
		t.Fatalf("legacy replay changed: %s %v %v", got, changed, err)
	}
}

func TestPlanStorySetSizing_ContentRevisionAndReplay(t *testing.T) {
	t.Parallel()
	s := PlanState{}
	entries := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "Do A", Size: "S", SizeReason: "One bounded change"}}
	first, changed, err := s.ProposeStorySet(entries)
	if err != nil || !changed {
		t.Fatalf("initial proposal: %v %v", changed, err)
	}
	for _, change := range []func(*PlanStorySetEntry){func(e *PlanStorySetEntry) { e.Size = "M" }, func(e *PlanStorySetEntry) { e.SizeReason = "Several related changes" }} {
		previous := s.StorySet.Revision
		change(&entries[0])
		next, changed, err := s.ProposeStorySet(entries)
		if err != nil || !changed || next == previous || next == first {
			t.Fatalf("sizing revision: %s %v %v", next, changed, err)
		}
		if replay, changed, err := s.ProposeStorySet(entries); err != nil || changed || replay != next {
			t.Fatalf("replay changed: %s %v %v", replay, changed, err)
		}
		if _, err := s.PromoteStorySet(previous); !errors.Is(err, ErrPlanStorySetStaleRevision) {
			t.Fatalf("stale sizing approval: %v", err)
		}
	}
	if _, err := s.PromoteStorySet(s.StorySet.Revision); err != nil {
		t.Fatal(err)
	}
	entries[0].Size = "L"
	if _, _, err := s.ProposeStorySet(entries); !errors.Is(err, ErrPlanStorySetPromoted) {
		t.Fatalf("promoted sizing replaced: %v", err)
	}
}

func TestPlanStorySetSizing_GapsAreAdvisoryAndLegacyIsExempt(t *testing.T) {
	t.Parallel()
	entries := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "A"}, {ID: "b", Name: "B", Intent: "B", Size: "XL", SizeReason: "  "}, {ID: "c", Name: "C", Intent: "C", Size: "M", SizeReason: "Several changes"}}
	s := PlanState{}
	rev, _, err := s.ProposeStorySet(entries)
	if err != nil {
		t.Fatal(err)
	}
	gaps := s.StorySet.SizingGaps()
	want := []string{"a/size", "a/size_reason", "b/size", "b/size_reason"}
	var got []string
	for _, g := range gaps {
		got = append(got, g.EntryID+"/"+g.Field)
		if g.Expected == "" {
			t.Fatal("gap has no expected value")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gaps=%v want %v", got, want)
	}
	if _, err := s.PromoteStorySet(rev); err != nil {
		t.Fatalf("advisory gaps blocked promotion: %v", err)
	}
	legacy := s.StorySet.Clone()
	legacy.SizingPolicyVersion = 0
	if legacy.SizingApplicable() || len(legacy.SizingGaps()) != 0 {
		t.Fatal("legacy set acquired obligation")
	}
}

func TestPlanStorySetSizing_PolicyAndValuesSurviveStateOperations(t *testing.T) {
	t.Parallel()
	for _, legacy := range []bool{false, true} {
		name := "new"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entries := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "A", Size: "S", SizeReason: "  Original reason  "}}
			s := PlanState{}
			if legacy {
				s.StorySet = &PlanStorySetState{Revision: "old", Entries: []PlanStorySetEntry{{ID: "old", Name: "Old", Intent: "Old"}}}
			}
			if _, _, err := s.ProposeStorySet(entries); err != nil {
				t.Fatal(err)
			}
			policy := 0
			if !legacy {
				policy = PlanStorySetSizingPolicyVersion
			}
			for _, copy := range []*PlanStorySetState{s.StorySet.Clone()} {
				if copy.SizingPolicyVersion != policy || copy.Entries[0].SizeReason != entries[0].SizeReason {
					t.Fatalf("clone changed state: %#v", copy)
				}
			}
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if legacy && strings.Contains(string(raw), "sizing_policy_version") {
				t.Fatal("legacy policy serialized")
			}
			var loaded PlanState
			if err = json.Unmarshal(raw, &loaded); err != nil {
				t.Fatal(err)
			}
			entries[0].Intent = "Changed intent"
			rev, _, err := loaded.ProposeStorySet(entries)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = loaded.PromoteStorySet(rev); err != nil {
				t.Fatal(err)
			}
			loaded.Invalidate("story-001", "story", "")
			if loaded.StorySet.SizingPolicyVersion != policy || loaded.StorySet.SizingApplicable() == legacy {
				t.Fatal("policy changed across replacement/promotion/invalidation")
			}
			e, ok := loaded.StorySet.EntryForStory("story-001")
			if !ok || e.Size != "S" || e.SizeReason != "  Original reason  " {
				t.Fatalf("lookup lost sizing: %#v %v", e, ok)
			}
			if _, ok := loaded.StorySet.EntryForStory("missing"); ok {
				t.Fatal("unknown story found")
			}
		})
	}
}

func TestPlanStorySetSizing_RenderLegacyAndVoluntarySizing(t *testing.T) {
	t.Parallel()
	s := &PlanStorySetState{Revision: "old", Entries: []PlanStorySetEntry{{ID: "a", Name: "First", Intent: "Do first"}, {ID: "b", Name: "Second", Intent: "Do second", DependsOn: []string{"a"}}}}
	rendered, err := RenderPlanStorySet(s)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(rendered, "| ID |")
	if start < 0 {
		t.Fatalf("table missing: %s", rendered)
	}
	end := strings.Index(rendered[start:], "\n\n")
	if end < 0 {
		t.Fatalf("table missing: %s", rendered)
	}
	const oldTable = "| ID | Name | Description | Dependencies |\n|---|---|---|---|\n| `a` | First | Do first | none |\n| `b` | Second | Do second | `a` |"
	if got := rendered[start : start+end]; got != oldTable {
		t.Fatalf("legacy table changed:\n%s", got)
	}
	s.Entries[0].Size = "S"
	s.Entries[0].SizeReason = "One bounded change"
	rendered, err = RenderPlanStorySet(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "| Size | Size reason |") || !strings.Contains(rendered, "| S | One bounded change |") {
		t.Fatalf("voluntary sizing missing: %s", rendered)
	}
	if strings.Contains(rendered, "correction requested") {
		t.Fatal("legacy voluntary sizing acquired obligation")
	}
}

func TestPlanStorySetSizing_RenderAdvisoryCellsAndEscapeValues(t *testing.T) {
	t.Parallel()
	s := &PlanStorySetState{Revision: "new", SizingPolicyVersion: PlanStorySetSizingPolicyVersion, Entries: []PlanStorySetEntry{
		{ID: "missing", Name: "Missing", Intent: "Missing sizing"},
		{ID: "invalid", Name: "Invalid", Intent: "Invalid sizing", Size: "X|L\nnext", SizeReason: "  "},
		{ID: "supplied", Name: "Supplied", Intent: "Supplied sizing", Size: "S", SizeReason: "Reason | details\ncontinued"},
	}}
	rendered, err := RenderPlanStorySet(s)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(rendered, "\n") {
		for _, id := range []string{"missing", "invalid", "supplied"} {
			if strings.HasPrefix(line, "| `"+id+"` |") {
				rows[id] = line
			}
		}
	}
	if strings.Count(rows["missing"], "correction requested") != 2 || !strings.Contains(rows["missing"], "| missing; correction requested:") {
		t.Fatalf("missing cells lack correction: %s", rows["missing"])
	}
	if strings.Count(rows["invalid"], "correction requested") != 2 || !strings.Contains(rows["invalid"], `X\|L next (invalid); correction requested:`) {
		t.Fatalf("invalid cell not escaped or flagged: %s", rows["invalid"])
	}
	if !strings.Contains(rows["supplied"], `| S | Reason \| details continued |`) || strings.Contains(rows["supplied"], "correction requested") {
		t.Fatalf("supplied cells changed shape: %s", rows["supplied"])
	}
	if len(rows) != 3 {
		t.Fatalf("sizing values broke table rows: %v", rows)
	}
}

package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// AC-006 (story-002): facts render one per line, in fixed stage order
// (analysis, repair, checks, delivery, overflow last), keeping each stage's
// own recorded order stable.
func TestBugFactLines_StageOrder(t *testing.T) {
	t.Parallel()
	facts := []store.BugFact{
		{Stage: store.BugFactStageDelivery, Reference: "d1", Text: "delivery fact"},
		{Stage: store.BugFactStageOverflow, Reference: "overflow", Text: "3 further facts were dropped"},
		{Stage: store.BugFactStageAnalysis, Reference: "a1", Text: "first analysis fact"},
		{Stage: store.BugFactStageChecks, Reference: "c1", Text: "checks fact"},
		{Stage: store.BugFactStageAnalysis, Reference: "a2", Text: "second analysis fact"},
		{Stage: store.BugFactStageRepair, Reference: "r1", Text: "repair fact"},
	}
	got := bugFactLines(facts)
	want := []string{
		"[analysis] first analysis fact",
		"[analysis] second analysis fact",
		"[repair] repair fact",
		"[checks] checks fact",
		"[delivery] delivery fact",
		"[overflow] 3 further facts were dropped",
	}
	if len(got) != len(want) {
		t.Fatalf("bugFactLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBugFactLines_Empty(t *testing.T) {
	t.Parallel()
	if got := bugFactLines(nil); len(got) != 0 {
		t.Fatalf("empty facts produced lines: %v", got)
	}
}

// bugFactLines must never mutate the caller's slice order (it copies before
// sorting).
func TestBugFactLines_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	facts := []store.BugFact{
		{Stage: store.BugFactStageDelivery, Text: "d"},
		{Stage: store.BugFactStageAnalysis, Text: "a"},
	}
	original := append([]store.BugFact(nil), facts...)
	_ = bugFactLines(facts)
	for i := range facts {
		if facts[i] != original[i] {
			t.Fatalf("input slice was mutated: %v vs %v", facts, original)
		}
	}
}

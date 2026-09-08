package agent

import "testing"

func TestApprovedBundleCloneIsolatesEveryCollection(t *testing.T) {
	t.Parallel()
	original := &ApprovedBundle{ParentRecovery: true, Stories: []ApprovedStory{{Key: "US-1", Dependencies: []string{"US-0"}}}, CompletedDependencies: []CompletedStory{{Key: "US-0"}}}
	cloned := original.Clone()
	cloned.Stories[0].Key = "changed"
	cloned.Stories[0].Dependencies[0] = "changed"
	cloned.CompletedDependencies[0].Key = "changed"
	if original.Stories[0].Key != "US-1" || original.Stories[0].Dependencies[0] != "US-0" || original.CompletedDependencies[0].Key != "US-0" || !cloned.ParentRecovery {
		t.Fatal("cloned execution context shares mutable collections or drops recovery intent")
	}
	var absent *ApprovedBundle
	if absent.Clone() != nil {
		t.Fatal("nil clone created scope")
	}
}

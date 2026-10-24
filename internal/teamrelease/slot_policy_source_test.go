package teamrelease

import (
	"bytes"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// Only the editable Go story worker acquired a new initial cost. Compatibility
// comparisons with other teams and immutable history remove that one declared
// policy difference after checking its scope; every other graph byte still has
// to satisfy the existing comparison.
func withoutGoStoryWorkerSlotPolicy(t *testing.T, raw []byte) []byte {
	t.Helper()
	profile, err := graphexec.ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Team != "go-development" {
		t.Fatalf("slot policy source is team %q", profile.Team)
	}
	for _, node := range profile.Nodes {
		cost, err := graphexec.SlotCost(profile, node.Name)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if node.Terminal {
			want = 0
		}
		if node.Name == "worker_agent" {
			want = 3
			if node.Action != "lf_agent" || node.Attrs["lf_agent_markdown"] != "story-worker" || node.Attrs["lf_slot_budget"] != "3" {
				t.Fatalf("unexpected Go worker policy: %+v", node)
			}
		} else if _, supplied := node.Attrs["lf_slot_budget"]; supplied {
			t.Fatalf("unexpected slot policy on %s", node.Name)
		}
		if cost != want {
			t.Fatalf("%s cost=%d want=%d", node.Name, cost, want)
		}
	}
	prefix := []byte(`worker_agent  [lf_slot_budget="3", `)
	if bytes.Count(raw, prefix) != 1 || bytes.Count(raw, []byte("lf_slot_budget")) != 1 {
		t.Fatal("Go worker policy must be the only slot declaration")
	}
	return bytes.Replace(raw, prefix, []byte("worker_agent  ["), 1)
}

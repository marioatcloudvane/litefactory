package team

import (
	"strings"
	"testing"
)

func TestModeRosterPreservesDeclaredOrder(t *testing.T) {
	t.Parallel()
	document := strings.Replace(validIndex, `[{"ref":"reader"}]`, `[{"id":"z-writer","desc":"Write","path":"writer.md","tier":"fast"},{"ref":"reader"},{"id":"a-reviewer","desc":"Review","path":"reviewer.md","tier":"flagship"}]`, 1)
	idx, err := Decode([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	roster, err := idx.Roster("go-team", "research")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"z-writer", "reader", "a-reviewer"}
	if len(roster) != len(want) {
		t.Fatalf("roster length = %d; want %d", len(roster), len(want))
	}
	for i, id := range want {
		if roster[i].ID != id {
			t.Fatalf("roster[%d] = %q; want %q", i, roster[i].ID, id)
		}
	}
}

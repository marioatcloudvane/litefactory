package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
)

func TestConflictSnapshotsRemainIndependentAndAggregateRefusals(t *testing.T) {
	var snapshots []git.ConflictSnapshot
	response := agent.ConflictResolution{Version: 1}
	for _, path := range []string{"a.go", "b.go"} {
		original := "<<<<<<< a\npackage a\n=======\npackage b\n>>>>>>> b\n"
		regions, err := git.ParseConflictRegions(path, original)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, git.ConflictSnapshot{Path: path, Original: original, Regions: regions})
		text := "invalid source"
		response.Files = append(response.Files, agent.ConflictResolutionFile{Path: path, Regions: []agent.ConflictRegionResolution{{ID: regions[0].ID, Resolution: &text}}})
	}
	authority := conflictAdvisorFiles(snapshots)
	request := conflictAdvisorFiles(snapshots)
	request[0].Regions[0].ID = "tampered"
	if authority[0].Regions[0].ID != snapshots[0].Regions[0].ID {
		t.Fatal("request mutated authority")
	}
	if _, err := validateCollectedConflictResolution(snapshots, response); err == nil || !strings.Contains(err.Error(), "a.go") || !strings.Contains(err.Error(), "b.go") {
		t.Fatalf("missing independent refusals: %v", err)
	}
}

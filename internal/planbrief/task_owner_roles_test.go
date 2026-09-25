package planbrief

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Task owners are the Implement roster's specialists. Until they are read from
// the admitted team's roster (#319), the list must match the shipped go-team
// roster: adding or removing an Implement specialist there has to reach it.
func TestTaskOwnerScopeMatchesTheShippedImplementRoster(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "teams", "source", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Teams map[string]struct {
			Implement []struct {
				ID  string `json:"id"`
				Ref string `json:"ref"`
			} `json:"implement"`
		} `json:"teams"`
	}
	if err = json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	team, ok := index.Teams["go-team"]
	if !ok {
		t.Fatal("the shipped index has no go-team")
	}
	var specialists []string
	for _, entry := range team.Implement {
		name := entry.ID
		if name == "" {
			name = entry.Ref
		}
		if name != "implement-orchestrator" {
			specialists = append(specialists, name)
		}
	}
	got := TaskOwnerScope()
	if !slices.Equal(got.Roles, specialists) || got.IssueRefs == nil || len(got.IssueRefs) != 0 {
		t.Fatalf("task owner scope %+v, want the go-team implement specialists %v", got, specialists)
	}
	// Callers get their own copy: changing it never changes the next scope.
	got.Roles[0] = "changed"
	if TaskOwnerScope().Roles[0] == "changed" {
		t.Fatal("TaskOwnerScope shares its role list with callers")
	}
}

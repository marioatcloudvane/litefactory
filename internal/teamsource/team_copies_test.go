package teamsource

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The owner edits roles in internal/teamsource, the copy the factory runs.
// teams/source must follow it. When the two differ, the fix is always to copy
// internal/teamsource over teams/source; resetting internal/teamsource to the
// older copy silently undoes the owner's edits (56b2459 did exactly that).
func TestTeamSourceCopiesFollowTheLiveCopy(t *testing.T) {
	for _, team := range []string{"bug", "plan"} {
		live := filepath.Join("..", "..", "internal", "teamsource", team)
		mirror := filepath.Join("..", "..", "teams", "source", team)
		err := filepath.WalkDir(live, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
				return err
			}
			rel, err := filepath.Rel(live, path)
			if err != nil {
				return err
			}
			want, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			got, err := os.ReadFile(filepath.Join(mirror, rel))
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("teams/source/%s/%s differs from internal/teamsource/%[1]s/%[2]s. internal/teamsource is the live copy the owner edits: copy it over teams/source (cp internal/teamsource/%[1]s/%[2]s teams/source/%[1]s/%[2]s). Never reset internal/teamsource to match teams/source; that undoes the owner's edits.", team, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

package teambuild

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/teamsource"
)

// sharedSkillFiles reads the base's shared skill folder, keyed by the path
// below it (for example "run-rules/SKILL.md").
func sharedSkillFiles(t *testing.T, source string) map[string][]byte {
	t.Helper()
	dir := filepath.Join(source, "base-team", filepath.FromSlash(teamsource.SharedSkillsDir))
	files := snapshot(t, dir)
	if len(files) == 0 {
		t.Fatalf("no shared skills under %s", dir)
	}
	return files
}

// Every workflow of every team receives each shared skill exactly as it is in
// teams/source, and the shared folder itself is not installed.
func TestEveryWorkflowOfEveryTeamReceivesTheSharedSkills(t *testing.T) {
	shared := sharedSkillFiles(t, repoSource())
	for _, team := range teams {
		t.Run(team, func(t *testing.T) {
			files, _, err := Build(t.Context(), repoSource(), team)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range Workflows {
				for name, raw := range shared {
					got, ok := files[w+"/skills/"+filepath.ToSlash(name)]
					if !ok {
						t.Errorf("%s/skills/%s: shared skill missing", w, name)
						continue
					}
					if !bytes.Equal(got, raw) {
						t.Errorf("%s/skills/%s: differs from the shared source", w, name)
					}
				}
			}
			for name := range files {
				if strings.HasPrefix(name, "shared/") {
					t.Errorf("%s: the shared folder itself is installed", name)
				}
			}
		})
	}
}

// A workflow file at a shared skill path would be a second source of that
// skill, so the build refuses it.
func TestWorkflowCopyOfASharedSkillIsRefused(t *testing.T) {
	source := copySource(t)
	var name string
	for n := range sharedSkillFiles(t, source) {
		name = n
		break
	}
	put(t, filepath.Join(source, "base-team", "bug", "skills", filepath.FromSlash(name)), []byte("---\nname: copy\ndescription: copy\n---\ncopy\n"))
	if _, _, err := Build(t.Context(), source, teams[0]); err == nil || !strings.Contains(err.Error(), "collides with shared skill") {
		t.Fatalf("a workflow copy of a shared skill was accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "base-team", "bug")); err != nil {
		t.Fatal(err)
	}
}

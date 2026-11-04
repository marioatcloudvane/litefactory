package teamrelease

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutOfGraphSkillClosureIsReleasedAndStraySkillRefused(t *testing.T) {
	skill := []byte("---\nname: brainstorm\ndescription: Fixture skill.\nresources:\n  - notes.md\n---\nFixture body.\n")
	notes := []byte("Fixture notes.\n")

	t.Run("listed skill released", func(t *testing.T) {
		root := fixture(t)
		dir := filepath.Join(root, "teams/source/go-development/research/skills/brainstorm")
		put(t, filepath.Join(dir, "SKILL.md"), skill)
		put(t, filepath.Join(dir, "notes.md"), notes)
		files, release, err := candidateSource(t.Context(), mustRoot(t, root), "teams/source/go-development")
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string][]byte{"research/skills/brainstorm/SKILL.md": skill, "research/skills/brainstorm/notes.md": notes} {
			if string(files[name]) != string(want) {
				t.Fatalf("%s not released faithfully: %q", name, files[name])
			}
			listed := false
			for _, f := range release.Files {
				listed = listed || f.Path == name
			}
			if !listed {
				t.Fatalf("%s missing from release inventory", name)
			}
		}
	})

	t.Run("unlisted stray skill refused", func(t *testing.T) {
		root := fixture(t)
		put(t, filepath.Join(root, "teams/source/go-development/research/skills/stray/SKILL.md"), []byte("---\nname: stray\ndescription: Fixture stray.\n---\nStray body.\n"))
		_, _, err := candidateSource(t.Context(), mustRoot(t, root), "teams/source/go-development")
		if err == nil || !strings.Contains(err.Error(), "research/skills/stray/SKILL.md: resource is outside the captured role closure") {
			t.Fatalf("stray skill not refused: %v", err)
		}
	})

	t.Run("unlisted stray resource of listed skill refused", func(t *testing.T) {
		root := fixture(t)
		dir := filepath.Join(root, "teams/source/go-development/research/skills/brainstorm")
		put(t, filepath.Join(dir, "SKILL.md"), skill)
		put(t, filepath.Join(dir, "notes.md"), notes)
		put(t, filepath.Join(dir, "undeclared.md"), notes)
		_, _, err := candidateSource(t.Context(), mustRoot(t, root), "teams/source/go-development")
		if err == nil || !strings.Contains(err.Error(), "research/skills/brainstorm/undeclared.md: resource is outside the captured role closure") {
			t.Fatalf("undeclared resource not refused: %v", err)
		}
	})
}

func mustRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

// outOfGraphPath reports a released file of a listed out-of-graph skill; the
// selected-workflow view exposes only what graph validation read.
func outOfGraphPath(name string) bool {
	for w, skills := range outOfGraphSkills {
		for _, skill := range skills {
			if strings.HasPrefix(name, w+"/skills/"+skill+"/") {
				return true
			}
		}
	}
	return false
}

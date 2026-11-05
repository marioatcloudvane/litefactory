package brainstorm

import (
	"errors"
	"io/fs"
	"testing"
)

func mapReader(files map[string]string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		raw, ok := files[name]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(raw), nil
	}
}

func TestCaptureTeamReadsRoleAndBothSkillClosures(t *testing.T) {
	files := map[string]string{
		"research.md":                          "---\nname: research\n---\nrole fixture\n",
		"skills/research-evidence/SKILL.md":    "---\nname: research-evidence\ndescription: fixture\nresources: [evidence.md]\n---\nevidence fixture\n",
		"skills/research-evidence/evidence.md": "resource fixture\n",
		"skills/brainstorm/SKILL.md":           "---\nname: brainstorm\ndescription: fixture\n---\nbrainstorm fixture\n",
		"skills/research-mode-bug/SKILL.md":    "---\nname: research-mode-bug\ndescription: fixture\n---\nnot captured\n",
	}
	capture, err := captureTeam(mapReader(files))
	if err != nil {
		t.Fatal(err)
	}
	if capture.Role != files["research.md"] || len(capture.Files) != 3 || capture.Files["skills/research-evidence/evidence.md"] != "resource fixture\n" || capture.Files["skills/brainstorm/SKILL.md"] == "" {
		t.Fatalf("capture = %+v", capture)
	}
	delete(files, "skills/brainstorm/SKILL.md")
	if _, err = captureTeam(mapReader(files)); !errors.Is(err, ErrSkillMissing) {
		t.Fatalf("missing skill = %v", err)
	}
}

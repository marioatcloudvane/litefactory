package instructionpackage

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestConversationFactoryCarriesTheStandardsAuthoringSkill(t *testing.T) {
	if !slices.Contains(selectedSkills("conversation-factory"), StandardsAuthoringSkill) {
		t.Fatal("standards-authoring is not selected for the conversation agent")
	}
	pkg, err := ShippedConversationPackage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	captured, _, err := pkg.CaptureConversation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := captured.Encode()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreConversation(raw)
	if err != nil {
		t.Fatal(err)
	}
	role := restored.roles[1]
	skill := string(role.SkillInstructions(StandardsAuthoringSkill))
	if skill == "" || restored.Factory() != string(role.Instructions())+"\n\n"+skill {
		t.Fatal("the captured skill does not follow the factory instructions")
	}
}

// Captures retained before the skill existed, and packages without it, keep
// working: their factory instructions are the role alone.
func TestConversationFactoryWithoutTheSkillStillCaptures(t *testing.T) {
	shipped, err := fs.Sub(embedded, "embedded")
	if err != nil {
		t.Fatal(err)
	}
	without := fstest.MapFS{}
	err = fs.WalkDir(shipped, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path, "skills/"+StandardsAuthoringSkill+"/") {
			return err
		}
		raw, err := fs.ReadFile(shipped, path)
		without[path] = &fstest.MapFile{Data: raw}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(t.Context(), without, "")
	if err != nil {
		t.Fatal(err)
	}
	captured, _, err := pkg.CaptureConversation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := captured.Encode()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreConversation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Factory() != string(restored.roles[1].Instructions()) {
		t.Fatal("a capture without the skill gained instructions")
	}
	if _, err = LegacyConversation(t.Context()); err != nil {
		t.Fatalf("legacy baseline no longer captures: %v", err)
	}
}

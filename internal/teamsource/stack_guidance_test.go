package teamsource

import (
	"bytes"
	"fmt"
	"github.com/cloudvane/litefactory/internal/config"
	"strings"
	"testing"
	"testing/fstest"
)

func stackGuidanceFixture() fstest.MapFS {
	return fstest.MapFS{
		"plan/skills/non-go/SKILL.md":         {Data: []byte("---\nname: non-go\ndescription: Stack advice\nrequires: [shared/base]\nresources: [guide.md]\n---\nRead [guide](guide.md).\n")},
		"plan/skills/non-go/guide.md":         {Data: []byte("Stack-specific retained advice.\n")},
		"plan/skills/shared/base/SKILL.md":    {Data: []byte("---\nname: base\ndescription: Shared advice\nresources: [contract.md]\n---\nShared contract.\n")},
		"plan/skills/shared/base/contract.md": {Data: []byte("Preserve factory results.\n")},
	}
}

func TestStackGuidanceBoundsEmptyResourceCount(t *testing.T) {
	source := fstest.MapFS{}
	var header strings.Builder
	header.WriteString("---\nname: stack\ndescription: Bounded guidance\nresources:\n")
	for i := range maxStackGuidanceFiles {
		name := fmt.Sprintf("resource-%d.md", i)
		header.WriteString("  - " + name + "\n")
		source["skills/stack/"+name] = &fstest.MapFile{}
	}
	header.WriteString("---\nAdvice.\n")
	source["skills/stack/SKILL.md"] = &fstest.MapFile{Data: []byte(header.String())}
	p := config.StackProfile{Version: 1, Guidance: []config.StackGuidance{{ID: "stack", Skill: "skills/stack/SKILL.md"}}}
	if got, err := CaptureStackGuidance(t.Context(), source, p); err == nil || got != nil {
		t.Fatal("zero-byte resources bypassed captured file bounds")
	}
}
func TestStackGuidanceCapturesSelectedSkillDependencyAndResources(t *testing.T) {
	source := stackGuidanceFixture()
	p := config.StackProfile{Version: 1, Guidance: []config.StackGuidance{{ID: "non-go", Skill: "plan/skills/non-go/SKILL.md"}}}
	got, err := CaptureStackGuidance(t.Context(), source, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(source) {
		t.Fatalf("closure files=%d want%d", len(got), len(source))
	}
	for name, file := range source {
		if !bytes.Equal(got[name], file.Data) {
			t.Fatalf("missing or altered %s", name)
		}
	}
	source["plan/skills/non-go/guide.md"].Data = []byte("edited")
	if string(got["plan/skills/non-go/guide.md"]) != "Stack-specific retained advice.\n" {
		t.Fatal("capture aliases mutable source bytes")
	}
}
func TestStackGuidanceRefusesIncompleteOrUnsafeClosure(t *testing.T) {
	for _, kind := range []string{"missing skill", "missing resource", "missing dependency", "escaping resource", "cycle", "invalid header"} {
		t.Run(kind, func(t *testing.T) {
			source := stackGuidanceFixture()
			p := config.StackProfile{Version: 1, Guidance: []config.StackGuidance{{ID: "non-go", Skill: "plan/skills/non-go/SKILL.md"}}}
			switch kind {
			case "missing skill":
				delete(source, p.Guidance[0].Skill)
			case "missing resource":
				delete(source, "plan/skills/non-go/guide.md")
			case "missing dependency":
				delete(source, "plan/skills/shared/base/SKILL.md")
			case "escaping resource":
				source[p.Guidance[0].Skill].Data = bytes.ReplaceAll(source[p.Guidance[0].Skill].Data, []byte("resources: [guide.md]"), []byte("resources: [../../../private-canary]"))
			case "cycle":
				source["plan/skills/shared/base/SKILL.md"].Data = bytes.ReplaceAll(source["plan/skills/shared/base/SKILL.md"].Data, []byte("description: Shared advice"), []byte("description: Shared advice\nrequires: [non-go]"))
			case "invalid header":
				source[p.Guidance[0].Skill].Data = []byte("---\nname: private-canary\nresources: [\n---\n")
			}
			got, err := CaptureStackGuidance(t.Context(), source, p)
			if err == nil || got != nil {
				t.Fatal("invalid guidance accepted")
			}
			if !strings.Contains(err.Error(), "stack.guidance[0]") || strings.Contains(err.Error(), "private-canary") {
				t.Fatal("missing field or exposed authored value", err)
			}
		})
	}
}

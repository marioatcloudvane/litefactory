package instructionpackage

import (
	"bytes"
	"testing"
)

func TestLogAnalystInstructionsIncludeSelectedSkills(t *testing.T) {
	p, err := LoadShipped(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := p.Capture(t.Context(), LogAnalystRole)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	retained, err := Restore(encoded)
	if err != nil {
		t.Fatal(err)
	}
	instructions, err := LogAnalystInstructions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go-guidance", "agent-setup-guidance"} {
		skill := retained.SkillInstructions(name)
		if len(skill) == 0 || !bytes.Contains(instructions, skill) {
			t.Fatalf("missing selected guidance %s", name)
		}
	}
	if bytes.Contains(instructions, []byte(`"id":"target ID for replacement"`)) {
		t.Fatal("role directs a forbidden revision field")
	}
}

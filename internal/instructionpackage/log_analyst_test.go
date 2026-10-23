package instructionpackage

import "testing"

func TestLogAnalystInstructionsLoad(t *testing.T) {
	instructions, err := LogAnalystInstructions(t.Context())
	if err != nil || len(instructions) == 0 {
		t.Fatalf("log analyst role unavailable: %v", err)
	}
}

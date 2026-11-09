package harness

import (
	"strings"
	"testing"
)

func TestOwnerGuidanceColdResumedAndWithdrawn(t *testing.T) {
	for _, resume := range []bool{false, true} {
		in := testPromptInput()
		in.Resume = resume
		in.OwnerGuidanceRevision = 7
		in.OwnerGuidanceBody = "Use ISO dates."
		prompt, err := BuildPrompt(in)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(prompt, "Use ISO dates.") != 1 || !strings.Contains(prompt, "Standing owner guidance (revision 7)") {
			t.Fatalf("standing direction missing from resume=%v prompt", resume)
		}
		in.OwnerGuidanceRevision = 8
		in.OwnerGuidanceBody = ""
		prompt, err = BuildPrompt(in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, "Standing owner guidance (revision 8)\n\n(cleared)") || strings.Contains(prompt, "Use ISO dates.") {
			t.Fatalf("withdrawal missing from resume=%v prompt", resume)
		}
	}
}

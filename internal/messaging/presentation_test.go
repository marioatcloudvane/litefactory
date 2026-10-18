package messaging

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
)

// TestNarrationPromptsSpeakToOwnerDirectly keeps narration in the second
// person, without third-person self-description, and relays a clarifying
// question once.
func TestNarrationPromptsSpeakToOwnerDirectly(t *testing.T) {
	outcome := presentation.FromSource("Which project do you mean?", presentation.Success, "English")
	candidate := presentation.Candidate{Claims: []presentation.Claim{{Text: "Which project do you mean?", Facts: []string{"result"}}}}
	writer, err := prompts.Load(t.Context(), nil, prompts.ReplyWriter, nil)
	if err != nil {
		t.Fatal(err)
	}
	generation := generationPromptWithInstructions(outcome, writer.Main)
	for _, want := range []string{
		"Answer the owner directly",
		"in its language hint",
		"in the second person",
		"Never describe the system, the answer, the result, the reply or the question in the third person",
		"If the result is itself a clarifying question, the narration is that question, asked once",
	} {
		if !strings.Contains(generation, want) {
			t.Errorf("generation prompt lacks %q", want)
		}
	}
	if strings.Contains(generation, "Explain only this completed command result") {
		t.Error("generation prompt still asks for a report about the result")
	}
	checker, err := prompts.Load(t.Context(), nil, prompts.ReplyChecker, nil)
	if err != nil {
		t.Fatal(err)
	}
	checking := checkingPromptWithInstructions(outcome, candidate, checker.Main)
	for _, want := range []string{
		"language inconsistent with the hint",
		"Mark as unsupported any claim that describes the system, the answer, the result, the reply or the question in the third person",
		"speaking to the owner directly in the second person",
		"repeats a clarifying question already asked",
	} {
		if !strings.Contains(checking, want) {
			t.Errorf("checking prompt lacks %q", want)
		}
	}
}

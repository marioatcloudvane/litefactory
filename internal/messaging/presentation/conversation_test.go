package presentation

import (
	"errors"
	"strings"
	"testing"
)

func TestConversationGroundingAndLosslessParts(t *testing.T) {
	explanation := strings.Repeat("seven widgets 😀 *unsafe* ", 250)
	fact := CurrentFact{ID: "current", Text: "Count: 7", Source: "meter", Target: "task 42", Caveats: "snapshot only", Complete: true}
	blocks := []AnswerBlock{{Text: explanation, FactRefs: []string{"current"}}, {FactRefs: []string{"current"}}, {Text: "Count: 7", FactRefs: []string{"foreign"}}}
	parts, err := RenderConversation("answer-1", blocks, []CurrentFact{fact}, "proposal bytes\nconfirm 42")
	if err != nil {
		t.Fatal(err)
	}
	var reconstructed strings.Builder
	for _, part := range parts[:len(parts)-4] {
		if messageSize(part.Content) > MessageLimit {
			t.Fatal("oversized part")
		}
		reconstructed.WriteString(part.Content)
	}
	if reconstructed.String() != plain(explanation) {
		t.Fatal("explanation lost content")
	}
	if parts[len(parts)-4].Content != "I couldn't get the latest information." || parts[len(parts)-3].Content != "Count: 7" || !strings.Contains(parts[len(parts)-2].Content, "could not be verified") {
		t.Fatalf("answer or warning lost: %+v", parts[len(parts)-4:])
	}
	last := parts[len(parts)-1]
	if !last.Protected || !strings.HasSuffix(last.Content, "proposal bytes\nconfirm 42") {
		t.Fatal("proposal changed")
	}
}

func TestConversationExactFactAndPartialRead(t *testing.T) {
	for _, complete := range []bool{true, false} {
		parts, err := RenderConversation("a", []AnswerBlock{{Text: "seven", FactRefs: []string{"f"}}}, []CurrentFact{{ID: "f", Text: "seven", Source: "s", Target: "t", Complete: complete}}, "")
		if err != nil {
			t.Fatal(err)
		}
		if parts[0].Content != "seven" {
			t.Fatal("incorrect grounding")
		}
		if !complete && (len(parts) != 2 || !strings.Contains(parts[1].Content, "incomplete")) {
			t.Fatal("missing incomplete-source warning")
		}
	}
	_, err := RenderConversation("a", nil, nil, strings.Repeat("😀", 1001))
	if !errors.Is(err, ErrProtectedProposalTooLarge) {
		t.Fatalf("wrong refusal: %v", err)
	}
}

func TestConversationMixedReferencesAndEscapes(t *testing.T) {
	parts, err := RenderConversation("a", []AnswerBlock{{FactRefs: []string{"known", "foreign"}}}, []CurrentFact{{ID: "known", Text: "fact", Complete: true}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].Content != "I couldn't get the latest information." {
		t.Fatal("unknown reference disappeared")
	}
	if end := splitEnd("abc\\*tail", 4); end != 3 {
		t.Fatalf("split escaped character at %d", end)
	}
	if end := splitEnd("abc\\\\tail", 5); end != 5 {
		t.Fatalf("split complete escape at %d", end)
	}
}

func TestConversationOrdinaryRepliesHaveNoBoilerplate(t *testing.T) {
	for _, intent := range []string{"answer", "clarify"} {
		parts, err := RenderConversation("saved-id", []AnswerBlock{{Text: "Which project?"}}, nil, "", intent)
		if err != nil || len(parts) != 1 || parts[0].Content != "Which project?" {
			t.Fatalf("%s: %+v %v", intent, parts, err)
		}
	}
	facts := []CurrentFact{{ID: "missing", Caveats: "Task status unavailable."}}
	parts, err := RenderConversation("a", []AnswerBlock{{Text: "Task status could not be read."}}, facts, "", "answer")
	if err != nil || parts[0].Content != "Task status could not be read." {
		t.Fatalf("lost local caveat: %+v %v", parts, err)
	}
	parts, err = RenderConversation("b", []AnswerBlock{{Text: "Which project?"}}, facts, "", "clarify")
	if err != nil || parts[0].Content != "Which project?" {
		t.Fatalf("clarification caveat: %+v %v", parts, err)
	}
}

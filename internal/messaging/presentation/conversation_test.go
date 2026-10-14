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
	for _, part := range parts {
		if messageSize(part.Content) > MessageLimit {
			t.Fatalf("oversized part: %d", messageSize(part.Content))
		}
		if strings.Contains(part.Content, "/details answer:") || strings.HasPrefix(part.Content, "Answer ") {
			t.Fatal("ordinary reply has metadata")
		}
		body := part.Content
		if strings.HasPrefix(body, "Caveat:") {
			text := strings.SplitN(body, "\n", 2)[1]
			if text != "Count: 7" {
				reconstructed.WriteString(text)
			}
		}
	}
	if reconstructed.String() != plain(explanation) {
		t.Fatal("explanation lost content")
	}
	if !strings.Contains(parts[len(parts)-3].Content, "Source: meter; target: task 42; caveats: snapshot only\nCount: 7") {
		t.Fatal("source or caveats lost")
	}
	if !strings.Contains(parts[len(parts)-2].Content, "Caveat:") {
		t.Fatal("foreign handle removed warning")
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
		if strings.Contains(parts[0].Content, "Caveat:") == complete {
			t.Fatal("incorrect grounding")
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
	if len(parts) != 2 || !strings.Contains(parts[1].Content, "Evidence unavailable.") || !strings.Contains(parts[1].Content, "foreign") {
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
	if err != nil || !strings.Contains(parts[0].Content, "Evidence limited: Task status unavailable.") {
		t.Fatalf("lost local caveat: %+v %v", parts, err)
	}
	parts, err = RenderConversation("b", []AnswerBlock{{Text: "Which project?"}}, facts, "", "clarify")
	if err != nil || parts[0].Content != "Which project?" {
		t.Fatalf("clarification caveat: %+v %v", parts, err)
	}
}

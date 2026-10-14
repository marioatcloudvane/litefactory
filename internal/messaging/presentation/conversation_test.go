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
		if !strings.Contains(part.Content, "/details answer:answer-1") {
			t.Fatal("missing selector")
		}
		body := strings.SplitN(part.Content, "\n\n", 2)[1]
		if strings.HasPrefix(body, "Unverified") {
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
	if !strings.Contains(parts[len(parts)-2].Content, "Unverified") {
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
		if strings.Contains(parts[0].Content, "Unverified") == complete {
			t.Fatal("incorrect grounding")
		}
	}
	_, err := RenderConversation("a", nil, nil, strings.Repeat("😀", 1000))
	if !errors.Is(err, ErrProtectedProposalTooLarge) {
		t.Fatalf("wrong refusal: %v", err)
	}
}

func TestConversationMixedReferencesAndEscapes(t *testing.T) {
	parts, err := RenderConversation("a", []AnswerBlock{{FactRefs: []string{"known", "foreign"}}}, []CurrentFact{{ID: "known", Text: "fact", Complete: true}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || !strings.Contains(parts[1].Content, "Unverified") || !strings.Contains(parts[1].Content, "foreign") {
		t.Fatal("unknown reference disappeared")
	}
	if end := splitEnd("abc\\*tail", 4); end != 3 {
		t.Fatalf("split escaped character at %d", end)
	}
	if end := splitEnd("abc\\\\tail", 5); end != 5 {
		t.Fatalf("split complete escape at %d", end)
	}
}

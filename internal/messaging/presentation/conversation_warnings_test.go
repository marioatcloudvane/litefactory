package presentation

import (
	"strings"
	"testing"
)

func TestConversationRetainsWordingAndGroundingWarnings(t *testing.T) {
	parts, err := RenderConversation("answer", []AnswerBlock{{Text: "The work may be ready.", FactRefs: []string{"unknown", "partial"}}}, []CurrentFact{{ID: "partial", Caveats: "The latest run was unavailable."}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || parts[0].Content != "The work may be ready." || !strings.Contains(parts[1].Content, "could not be verified") || !strings.Contains(parts[2].Content, "incomplete") || !strings.Contains(parts[2].Content, "The latest run was unavailable.") {
		t.Fatalf("missing retained wording or limitations: %+v", parts)
	}
	for _, part := range parts {
		if strings.Contains(part.Content, "unknown") || strings.Contains(part.Content, "partial") || part.Protected {
			t.Fatalf("warning exposes reference or proposal: %+v", part)
		}
	}
}

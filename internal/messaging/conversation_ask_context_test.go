package messaging

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationAskContextAdmissionAndBudget(t *testing.T) {
	prepared := ask.Prepared{Repository: store.Repo{Owner: "one", Name: "app"}, History: ask.History{Turns: []ask.Turn{
		{Project: "one/app", Question: "stored excerpt", Answer: "eligible", SourceID: "reply-1", Revision: "revision-1"},
		{Project: "other/app", Answer: "unrelated"},
	}}}
	raw, gaps, err := conversationAskContext(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "eligible") || !strings.Contains(raw, "reply-1") || strings.Contains(raw, "unrelated") || len(gaps) == 0 {
		t.Fatalf("unscoped context: %s", raw)
	}
	prepared.Bundle.TargetIssue = 9
	raw, gaps, err = conversationAskContext(prepared)
	if err != nil || strings.Contains(raw, "eligible") || !strings.Contains(strings.Join(gaps, " "), "excluded for an explicit issue") {
		t.Fatalf("explicit history: %s %v", raw, err)
	}
	prepared.Bundle.TargetIssue = 0
	prepared.History.Turns[0].Answer = strings.Repeat("x", 32<<10)
	prepared.Bundle.Facts = []ask.Fact{{Text: strings.Repeat("y", 32<<10)}}
	raw, gaps, err = conversationAskContext(prepared)
	if err != nil || len(raw) > 16384 || !json.Valid([]byte(raw)) || !strings.Contains(strings.Join(gaps, " "), "16 KiB") {
		t.Fatalf("unbounded context: size %d, %v", len(raw), err)
	}
}

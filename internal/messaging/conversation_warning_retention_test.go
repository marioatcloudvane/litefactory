package messaging

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationGroundingWarningRetainedAndProtected(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{})
	if err := run.s.db.SaveConversationSession(t.Context(), run.t, "saved-session"); err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(conversationReadResult{FactID: "partial", Gaps: []string{"private-gap unavailable"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.s.db.SaveConversationExchange(t.Context(), run.t, store.ConversationExchange{TurnID: run.t.ID, Ordinal: 1, Kind: "evidence", Result: string(result), CapturedAt: at}); err != nil {
		t.Fatal(err)
	}
	run.s.Protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "private-gap", "redacted"), nil
	}
	if err := run.s.saveConversationPresentation(t.Context(), run.t, "completed", []presentation.AnswerBlock{{Text: "The work may be ready.", FactRefs: []string{"missing", "partial"}}}, "", at); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	answer, _, err := run.s.db.ConversationReplyAnswer(t.Context(), reply)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := run.s.db.ReadConversationAnswerParts(t.Context(), answer.ID)
	if err != nil {
		t.Fatal(err)
	}
	var retained strings.Builder
	for _, part := range parts {
		retained.WriteString(part.Text)
	}
	text := retained.String()
	for _, wanted := range []string{"The work may be ready.", "could not be verified", "incomplete", "redacted unavailable"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("retained answer missing %q: %q", wanted, text)
		}
	}
	if strings.Contains(text, "private-gap") {
		t.Fatal("caveat retained without current redaction")
	}
}

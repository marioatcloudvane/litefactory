package discord

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOwnerReactionRequiresExactOwnerAndUnicodeEvidence(t *testing.T) {
	at := time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)
	valid := map[string]any{"user_id": "7", "channel_id": "3", "message_id": "99", "emoji": map[string]any{"id": nil, "name": "✅"}}
	raw, _ := json.Marshal(valid)
	r, ok := ownerReaction(raw, "7", []string{"3"}, at)
	if !ok || r.MessageID != "99" || !r.ReceivedAt.Equal(at) {
		t.Fatalf("missing exact evidence: %+v %t", r, ok)
	}
	for _, change := range []map[string]any{{"user_id": "8"}, {"channel_id": "4"}, {"message_id": ""}, {"emoji": map[string]any{"id": "123", "name": "✅"}}, {"emoji": map[string]any{"id": nil, "name": "☑️"}}, {"member": map[string]any{"user": map[string]any{"id": "7", "bot": true}}}} {
		input := map[string]any{}
		for k, v := range valid {
			input[k] = v
		}
		for k, v := range change {
			input[k] = v
		}
		raw, _ = json.Marshal(input)
		if _, ok = ownerReaction(raw, "7", []string{"3"}, at); ok {
			t.Fatalf("accepted changed evidence: %+v", change)
		}
	}
	if gatewayIntents&intentGuildReactions == 0 {
		t.Fatal("reaction intent disabled")
	}
}

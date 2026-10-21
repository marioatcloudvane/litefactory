package messaging_test

import (
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
)

// TestOnlyDetailsIsASlashCommand checks that /details is the only transport
// command and that other commands are never admitted.
func TestOnlyDetailsIsASlashCommand(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	send := &sender{outcome: messaging.Delivery{Confirmed: true}}
	s := messaging.New(db, "discord", "2", []string{"3", "4"}, send, messaging.FactoryStatus{DB: db})
	slash := s.SlashCommands()
	if len(slash) != 1 || slash[0].Name != "details" || !slash[0].NaturalLanguage || slash[0].Effect != messaging.ReadEffect || len(slash[0].Arguments) != 1 || slash[0].Arguments[0].Name != "answer" || slash[0].Arguments[0].Required {
		t.Fatalf("slash commands: %+v", slash)
	}
	for _, command := range []string{"help", "health", "status", "ask", "draft"} {
		allowed, inserted, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "9" + command, Channel: "3", Sender: "2", Command: command}, at)
		if err != nil || !allowed || inserted {
			t.Fatalf("%s admitted: %t %t %v", command, allowed, inserted, err)
		}
	}
	if allowed, _, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "8", Channel: "3", Sender: "5", Command: "details"}, at); err != nil || allowed {
		t.Fatalf("other sender allowed: %v", err)
	}
	step(t, s, at, false)
	if _, inserted, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "1", Channel: "4", Sender: "2", Command: "details"}, at); err != nil || !inserted {
		t.Fatalf("details not admitted: %t %v", inserted, err)
	}
	step(t, s, at, true)
	if len(send.texts) != 1 || send.texts[0] != "No answer details are available here." {
		t.Fatalf("details refusal: %q", send.texts)
	}
}

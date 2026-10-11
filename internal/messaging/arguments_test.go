package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
)

func TestArgumentsSurviveDeferredAdmission(t *testing.T) {
	db := database(t)
	at := time.Now().UTC()
	send := &sender{outcome: messaging.Delivery{Confirmed: true}}
	s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	called := false
	definition := messaging.CommandDefinition{Name: "echo", Description: "Read arguments", Arguments: []messaging.Argument{{Name: "page", Type: messaging.IntegerArgument, Description: "Page"}}}
	s.RegisterCommand(definition, func(_ context.Context, m messaging.Message, _ time.Time) (string, error) {
		called = true
		return m.Arguments["page"], nil
	})
	definition.Arguments[0].Name = "changed"
	args := map[string]string{"page": "2"}
	if allowed, inserted, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: "1", Channel: "channel", Sender: "owner", Command: "echo", Arguments: args}, at); err != nil || !allowed || !inserted {
		t.Fatalf("admit = %t, %t, %v", allowed, inserted, err)
	}
	args["page"] = "3"
	if called {
		t.Fatal("handler ran during admission")
	}
	// Reconstruct the service to prove arguments are not held in memory.
	restarted := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	restarted.RegisterCommand(messaging.CommandDefinition{Name: "echo"}, func(_ context.Context, m messaging.Message, _ time.Time) (string, error) {
		return m.Arguments["page"], nil
	})
	step(t, restarted, at, true)
	if len(send.texts) != 1 || send.texts[0] != "2" {
		t.Fatalf("restarted delivery = %v", send.texts)
	}
	row, err := db.ReadChatReply(t.Context(), "test", "1")
	if err != nil || len(row.Arguments) != 0 {
		t.Fatalf("terminal arguments = %v, %v", row.Arguments, err)
	}
	for _, def := range s.Declarations() {
		if def.Name == "echo" && def.Arguments[0].Name != "page" {
			t.Fatal("registry aliases caller arguments")
		}
	}
}

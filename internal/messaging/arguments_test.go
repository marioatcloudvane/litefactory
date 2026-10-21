package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
)

func TestRegistryCopiesArguments(t *testing.T) {
	db := database(t)
	at := time.Now().UTC()
	s := messaging.New(db, "test", "owner", []string{"channel"}, nil, messaging.FactoryStatus{DB: db})
	definition := messaging.CommandDefinition{Name: "echo", Description: "Read arguments", Arguments: []messaging.Argument{{Name: "page", Type: messaging.IntegerArgument, Description: "Page"}}}
	s.RegisterCommand(definition, func(_ context.Context, m messaging.Message, _ time.Time) (string, error) {
		return m.Arguments["page"], nil
	})
	definition.Arguments[0].Name = "changed"
	if text, _, err := s.RunCommand(t.Context(), messaging.Message{Transport: "test", ID: "1", Channel: "channel", Sender: "owner", Command: "echo", Arguments: map[string]string{"page": "2"}}, at); err != nil || text != "2" {
		t.Fatalf("handler = %q, %v", text, err)
	}
	for _, def := range s.Declarations() {
		if def.Name == "echo" && def.Arguments[0].Name != "page" {
			t.Fatal("registry aliases caller arguments")
		}
	}
}

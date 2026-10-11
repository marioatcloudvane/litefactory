package messaging_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging"
)

func TestNaturalInvocationCatalogueAndValidation(t *testing.T) {
	s := messaging.New(database(t), "test", "owner", []string{"channel"}, &sender{}, nil)
	s.EnableAsk(nil)
	s.EnableFeatures(nil, nil)
	s.EnableResearch(nil, nil)
	s.Register("unclassified", nil)
	s.RegisterCommand(messaging.CommandDefinition{Name: "bad-effect", NaturalLanguage: true, Effect: "model-decides"}, nil)
	want := map[string]messaging.CommandEffect{
		"help": messaging.ReadEffect, "health": messaging.ReadEffect, "status": messaging.ReadEffect,
		"meter": messaging.ReadEffect, "prices": messaging.ReadEffect, "ask": messaging.ReadEffect,
		"question": messaging.ReadEffect, "draft": messaging.ChangeEffect, "diagnose": messaging.ChangeEffect,
		"answer": messaging.ChangeEffect, "confirm": messaging.ChangeEffect,
	}
	for _, def := range s.NaturalCommands() {
		if want[def.Name] != def.Effect {
			t.Fatalf("unexpected routing metadata: %s %s", def.Name, def.Effect)
		}
		delete(want, def.Name)
		if len(def.Arguments) > 0 {
			def.Arguments[0].Name = "caller-edit"
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing catalogue entries: %v", want)
	}
	tests := []struct {
		name string
		in   messaging.Invocation
		bad  bool
	}{
		{"unclassified", messaging.Invocation{Command: "unclassified"}, true},
		{"unknown effect", messaging.Invocation{Command: "bad-effect"}, true},
		{"unknown argument", messaging.Invocation{Command: "health", Arguments: map[string]string{"tool": "write"}}, true},
		{"missing required", messaging.Invocation{Command: "ask"}, true},
		{"positive issue", messaging.Invocation{Command: "question", Arguments: map[string]string{"project": "o/r", "issue": "12"}}, false},
		{"zero issue", messaging.Invocation{Command: "question", Arguments: map[string]string{"project": "o/r", "issue": "0"}}, true},
		{"noncanonical issue", messaging.Invocation{Command: "question", Arguments: map[string]string{"project": "o/r", "issue": "+12"}}, true},
		{"overflow issue", messaging.Invocation{Command: "question", Arguments: map[string]string{"project": "o/r", "issue": "9999999999999999999999"}}, true},
		{"boolean", messaging.Invocation{Command: "meter", Arguments: map[string]string{"details": "true"}}, false},
		{"model boolean", messaging.Invocation{Command: "meter", Arguments: map[string]string{"details": "yes"}}, true},
		{"oversize note", messaging.Invocation{Command: "draft", Arguments: map[string]string{"note": strings.Repeat("x", 2001)}}, true},
		{"invalid text", messaging.Invocation{Command: "draft", Arguments: map[string]string{"note": string([]byte{255})}}, true},
		{"short code", messaging.Invocation{Command: "confirm", Arguments: map[string]string{"code": "123"}}, true},
		{"preview code", messaging.Invocation{Command: "confirm", Arguments: map[string]string{"code": "abcdefgh"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, effect, err := s.ValidateInvocation(tt.in)
			if (err != nil) != tt.bad || (tt.bad && !errors.Is(err, messaging.ErrInvalidInvocation)) {
				t.Fatalf("validation error = %v", err)
			}
			if tt.bad {
				if out.Command != "" || effect != "" {
					t.Fatal("invalid invocation escaped validation")
				}
				return
			}
			for name, value := range tt.in.Arguments {
				tt.in.Arguments[name] = "caller-edit"
				if out.Arguments[name] != value {
					t.Fatal("validated arguments alias caller data")
				}
			}
		})
	}
}

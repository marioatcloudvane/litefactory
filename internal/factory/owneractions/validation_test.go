package owneractions

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/owneraction"
)

func TestFixedActionValidationAndAvailability(t *testing.T) {
	available := Availability{Answer: true}
	invocation := owneraction.Invocation{Command: "answer", Arguments: map[string]string{"issue": "12", "revision": "abcdefgh1234", "text": "owner answer"}}
	validated, err := available.ValidateChange(invocation)
	if err != nil {
		t.Fatal(err)
	}
	invocation.Arguments["text"] = "changed by caller"
	if validated.Arguments["text"] != "owner answer" {
		t.Fatal("validated arguments alias caller input")
	}
	if _, err = (Availability{}).ValidateChange(invocation); !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("disabled operation accepted: %v", err)
	}
	if _, err = available.ValidateChange(owneraction.Invocation{Command: "diagnose", Arguments: map[string]string{"issue": "12"}}); !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("answer availability enabled diagnosis: %v", err)
	}
	catalog := available.ActionCatalog()
	if len(catalog) != 1 || catalog[0].Command != "answer" {
		t.Fatalf("discovery disagrees with availability: %+v", catalog)
	}
	// A presentation consumer cannot loosen the factory's required fields or types.
	fake := CommandDefinition{Name: "answer", Effect: ChangeEffect, Arguments: []Argument{{Name: "issue", Type: TextArgument}}}
	_, _, err = ValidateDefinition(fake, Invocation{Command: "answer", Arguments: map[string]string{"issue": "twelve"}})
	var argument *ArgumentError
	if !errors.As(err, &argument) || !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("consumer schema changed validation: %v", err)
	}
}

func TestActionFieldDiagnosticsKeepLimitsAndPrivateValues(t *testing.T) {
	_, _, err := (Availability{Standards: true}).ValidateInvocation(Invocation{Command: "standards-author", Arguments: map[string]string{"action": "edit", "rule": strings.Repeat("x", 32769)}})
	var argument *ArgumentError
	if !errors.As(err, &argument) || argument.Name != "rule" || argument.Limit != 32768 {
		t.Fatalf("protected standards limit changed: %v", err)
	}
	_, _, err = (Availability{Research: true}).ValidateInvocation(Invocation{Command: "diagnose", Arguments: map[string]string{"issue": "12", "note": strings.Repeat("x", 8192)}})
	if err != nil {
		t.Fatalf("diagnosis note budget changed: %v", err)
	}
	_, _, err = (Availability{Features: true}).ValidateInvocation(Invocation{Command: "draft", Arguments: map[string]string{"private-owner-text": "private-value"}})
	if !errors.Is(err, ErrInvalidInvocation) || strings.Contains(err.Error(), "private-owner-text") || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("unknown argument disclosed input: %v", err)
	}
}

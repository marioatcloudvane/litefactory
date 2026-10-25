package messaging

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func standardsFieldService(t *testing.T) *Service {
	t.Helper()
	s := New(naturalExecutionDB(t), "discord", "1", nil, nil, FactoryStatus{})
	s.EnableStandards(&standardsGateOperations{})
	return s
}

func standardsEditEnvelope(t *testing.T, args ...ConversationArgument) []byte {
	t.Helper()
	raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "propose", Command: "standards-proposal", Arguments: append([]ConversationArgument{{Name: "action", Value: "edit"}}, args...)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStandardsProseFieldTakesALongOwnerSpan(t *testing.T) {
	s := standardsFieldService(t)
	prefix := "proposal-1 version 2, set the description to: "
	description := strings.Repeat("Every handler logs its request id before it returns. ", 60)
	text := prefix + description
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "1", Text: text}}, Commands: s.ConversationCommands()}
	span := func(name string, start, end int) ConversationArgument {
		return ConversationArgument{Name: name, SourceID: "1", Start: start, End: end}
	}
	raw := standardsEditEnvelope(t, span("id", 0, len("proposal-1")), span("version", len("proposal-1 version "), len("proposal-1 version 2")), span("description", len(prefix), len(text)))
	_, inv, err := DecodeConversationEnvelope(raw, request)
	if err != nil || inv.Arguments["description"] != description || len(description) < 3000 {
		t.Fatalf("long description span: %d bytes, %v", len(inv.Arguments["description"]), err)
	}
	if _, _, err = s.ValidateInvocation(inv); err != nil {
		t.Fatalf("long description invalid: %v", err)
	}

	// The same text written by the model as a plain value is refused.
	raw = standardsEditEnvelope(t, span("id", 0, len("proposal-1")), span("version", len("proposal-1 version "), len("proposal-1 version 2")), ConversationArgument{Name: "description", Value: description})
	_, _, err = DecodeConversationEnvelope(raw, request)
	var rejected *ConversationEnvelopeError
	if !errors.As(err, &rejected) || rejected.Argument == nil || rejected.Argument.Name != "description" || !rejected.Ungrounded {
		t.Fatalf("plain description accepted: %v", err)
	}
	if reply := conversationCauseReply(err, request.Now); !strings.Contains(reply, "word for word") {
		t.Fatalf("plain description reply %q", reply)
	}
}

func TestStandardsShortFieldsKeepTheDefaultLimit(t *testing.T) {
	s := standardsFieldService(t)
	_, _, err := s.ValidateInvocation(Invocation{Command: "standards-proposal", Arguments: map[string]string{"action": "edit", "id": "proposal-1", "version": "2", "category_id": strings.Repeat("c", 300)}})
	var arg *ArgumentError
	if !errors.As(err, &arg) || arg.Name != "category_id" || arg.Limit != 256 {
		t.Fatalf("long category_id: %v", err)
	}
	for _, name := range []string{"rule", "description", "evidence", "scope"} {
		if argumentLimit("standards-author", name) != 32768 || !conversationProtectedArgument("standards-author", name) {
			t.Fatalf("%s is not standards owner content", name)
		}
	}
	for _, name := range []string{"category_id", "check", "applies_to"} {
		if argumentLimit("standards-proposal", name) != 256 || conversationProtectedArgument("standards-proposal", name) {
			t.Fatalf("%s changed", name)
		}
	}
}

func TestStandardsProseNamesStayOrdinaryElsewhere(t *testing.T) {
	for _, name := range []string{"rule", "description", "evidence", "scope"} {
		if conversationProtectedArgument("diagnose", name) || argumentLimit("diagnose", name) != 256 {
			t.Fatalf("%s is protected outside standards", name)
		}
	}
}

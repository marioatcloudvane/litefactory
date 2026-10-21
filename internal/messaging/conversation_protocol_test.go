package messaging

import (
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"strings"
	"testing"
	"time"
)

func TestConversationEnvelopeBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "s1", Text: "diagnose 42 stuck"}}, OwnerID: "owner", ChannelID: "channel", Now: now, Commands: []CommandDefinition{
		{Name: "diagnose", NaturalLanguage: true, Effect: ChangeEffect, Arguments: []Argument{{Name: "issue", Type: IntegerArgument, Required: true}, {Name: "note", Type: TextArgument}}},
		{Name: "status", NaturalLanguage: true, Effect: ReadEffect},
	}, References: []ConversationReference{{ID: "ref", OwnerID: "owner", ChannelID: "channel", OriginTurn: "turn1", ExpiresAt: now.Add(time.Hour), Arguments: map[string]string{"issue": "42", "note": "invented"}}}}
	tests := []struct {
		name, raw string
		valid     bool
	}{
		{"blocks", `{"version":1,"kind":"answer","blocks":[{"text":"seven widgets","fact_refs":["current"]},{"fact_refs":["current"]}]}`, false},
		{"mixed answer", `{"version":1,"kind":"answer","text":"a","blocks":[{"text":"b"}]}`, false},
		{"empty block", `{"version":1,"kind":"answer","blocks":[{}]}`, false},
		{"model support", `{"version":1,"kind":"answer","blocks":[{"text":"a","supported":true}]}`, false},
		{"answer", `{"version":1,"kind":"answer","text":"Current work is running."}`, true},
		{"lookup", `{"version":1,"kind":"lookup","command":"status","arguments":[]}`, true},
		{"proposal source", `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","source_id":"s1","start":9,"end":11},{"name":"note","source_id":"s1","start":12,"end":17}]}`, true},
		{"proposal reference", `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","reference":"ref"}]}`, true},
		{"duplicate", `{"version":1,"kind":"answer","text":"a","text":"b"}`, false},
		{"nested duplicate", `{"version":1,"kind":"lookup","command":"status","arguments":[{"name":"issue","name":"note","value":"42"}]}`, false},
		{"null", `{"version":1,"kind":"answer","text":null}`, false},
		{"unknown", `{"version":1,"kind":"answer","text":"a","owner":"owner"}`, false},
		{"execute", `{"version":1,"kind":"execute","command":"diagnose","arguments":[]}`, false},
		{"effect", `{"version":1,"kind":"lookup","command":"diagnose","arguments":[]}`, false},
		{"scalar proposal", `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","value":"42"}]}`, false},
		{"remembered content", `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","reference":"ref"},{"name":"note","reference":"ref"}]}`, false},
		{"mixed forms", `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","reference":"ref","value":"42"}]}`, false},
		{"unknown source", `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","source_id":"s2","start":9,"end":11}]}`, false},
		{"trailing", `{"version":1,"kind":"answer","text":"a"} {}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeConversationEnvelope([]byte(tt.raw), request)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, err)
			}
		})
	}
	// Bound source expansion across the whole answer, including repeated handles.
	excessive := ConversationEnvelope{Version: 1, Kind: "answer", Blocks: []presentation.AnswerBlock{{FactRefs: strings.Fields(strings.Repeat("current ", 17))}, {FactRefs: strings.Fields(strings.Repeat("current ", 16))}}}
	encoded, err := json.Marshal(excessive)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = DecodeConversationEnvelope(encoded, request); err == nil {
		t.Fatal("accepted unbounded fact expansion")
	}
	raw := []byte(`{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","reference":"ref"}]}`)
	for _, change := range []func(*ConversationReference){func(r *ConversationReference) { r.OwnerID = "other" }, func(r *ConversationReference) { r.ChannelID = "other" }, func(r *ConversationReference) { r.ExpiresAt = now }, func(r *ConversationReference) { r.OriginTurn = "" }} {
		clone := request
		clone.References = append([]ConversationReference(nil), request.References...)
		change(&clone.References[0])
		if _, _, err := DecodeConversationEnvelope(raw, clone); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
}

func TestConversationArgumentZeroOffset(t *testing.T) {
	raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "propose", Command: "draft", Arguments: []ConversationArgument{{Name: "note", SourceID: "s", Start: 0, End: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "s", Text: "idea"}}, Commands: []CommandDefinition{{Name: "draft", NaturalLanguage: true, Effect: ChangeEffect, Arguments: []Argument{{Name: "note", Type: TextArgument}}}}}
	_, inv, err := DecodeConversationEnvelope(raw, request)
	if err != nil || inv.Arguments["note"] != "idea" {
		t.Fatalf("span round trip failed: %v", err)
	}
}

func TestConversationExplicitTargetWinsHistory(t *testing.T) {
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "old", Text: "42"}, {SourceID: "new", Text: "diagnose #43"}}, Commands: []CommandDefinition{{Name: "diagnose", NaturalLanguage: true, Effect: ChangeEffect, Arguments: []Argument{{Name: "issue", Type: IntegerArgument, Required: true}}}}}
	raw := []byte(`{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","source_id":"old","start":0,"end":2}]}`)
	if _, _, err := DecodeConversationEnvelope(raw, request); err == nil {
		t.Fatal("previous target replaced current explicit target")
	}
}

func TestConversationEnvelopeGuardsRecordsNotWords(t *testing.T) {
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "s1", Text: "where is 728 at?"}}, Commands: []CommandDefinition{{Name: "status", NaturalLanguage: true, Effect: ReadEffect, Arguments: []Argument{{Name: "issue", Type: IntegerArgument}}}}}
	answer := func(text string) string {
		raw, _ := json.Marshal(ConversationEnvelope{Version: 1, Kind: "answer", Blocks: []presentation.AnswerBlock{{Text: text, FactRefs: []string{"t1-fact-3"}}}})
		return string(raw)
	}
	for _, text := range []string{
		"The three most recent runs (8, 9 and 10) were all planning runs.",
		"It is in implement mode, at a stage that is still listed as running.",
		"There is no evidence of a failure; the research question is still unanswered.",
		"Story #990 is merged; see the page for the details.",
	} {
		if _, _, err := DecodeConversationEnvelope([]byte(answer(text)), request); err != nil {
			t.Errorf("plain answer rejected: %q: %v", text, err)
		}
	}
	for _, text := range []string{
		"github:acme/litefactory#728: failed.",
		"Stage: await (running); story-001: acceptance.",
		"Recent runs (newest first, limit 3): 10: succeeded (plan);",
		"See dd8754eb1070a35be1e0fa6b1beac0f3-fact-3.",
		"Source: factory:status.",
		"Results are on page 1 of 3.",
	} {
		_, _, err := DecodeConversationEnvelope([]byte(answer(text)), request)
		if !errors.Is(err, ErrConversationEnvelope) || conversationEnvelopeReason(err) == "" {
			t.Errorf("raw record accepted or unexplained: %q: %v", text, err)
		}
	}
}

func TestConversationEnvelopeExtractsSingleObject(t *testing.T) {
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "s1", Text: "where is 728 at?"}}, Commands: []CommandDefinition{{Name: "status", NaturalLanguage: true, Effect: ReadEffect, Arguments: []Argument{{Name: "issue", Type: IntegerArgument}}}}}
	lookup := `{"version":1,"kind":"lookup","command":"status","arguments":[{"name":"issue","value":"728"}]}`
	for _, raw := range []string{
		"<invoke name=\"x\">\n</invoke>\n```\n\nLet me read the current status first.\n\n" + lookup,
		"```json\n" + lookup + "\n```",
		"  " + lookup + "\n",
	} {
		_, inv, err := DecodeConversationEnvelope([]byte(raw), request)
		if err != nil || inv.Command != "status" || inv.Arguments["issue"] != "728" {
			t.Errorf("single object not extracted from %q: %+v %v", raw, inv, err)
		}
	}
	for _, raw := range []string{
		lookup + "\n" + `{"version":1,"kind":"answer","text":"done"}`,
		"First {\"note\":1} then " + lookup,
		"no protocol object here",
	} {
		if _, _, err := DecodeConversationEnvelope([]byte(raw), request); !errors.Is(err, ErrConversationEnvelope) {
			t.Errorf("ambiguous output accepted: %q: %v", raw, err)
		}
	}
}

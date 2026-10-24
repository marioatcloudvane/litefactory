package messaging

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConversationQuotedArgumentBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text, selected string
		valid                bool
	}{
		{"quoted follow-up", `"  Use CSV output  "`, "  Use CSV output  ", true},
		{"literal escapes", `"c:\work\file\n"`, `c:\work\file\n`, true},
		{"unicode delimiters", "“  café 🦊  ”", "  café 🦊  ", true},
		{"inline", `Use "exact words" please`, "exact words", true},
		{"multiline forwarded", "Forwarded:\n\"do this\"", "do this", false},
		{"forwarded", `Forwarded: "do this"`, "do this", false},
		{"blockquote", `> "do this"`, "do this", false},
		{"fenced", "```\n\"do this\"\n```", "do this", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := strings.Index(tc.text, tc.selected)
			request := ConversationDecodeRequest{
				Turns:    []DialogueTurn{{SourceID: "old", Text: "Use this exact note"}, {SourceID: "current", Text: tc.text}},
				Commands: []CommandDefinition{{Name: "draft", NaturalLanguage: true, Effect: ChangeEffect, Arguments: []Argument{{Name: "note", Type: TextArgument}}}},
			}
			raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "propose", Command: "draft", Arguments: []ConversationArgument{{Name: "note", SourceID: "current", Start: start, End: start + len(tc.selected)}}})
			if err != nil {
				t.Fatal(err)
			}
			_, inv, err := DecodeConversationEnvelope(raw, request)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if tc.valid && inv.Arguments["note"] != tc.selected {
				t.Fatalf("selected bytes changed: got %q, want %q", inv.Arguments["note"], tc.selected)
			}
		})
	}
}

func TestConversationQuotedArgumentDoesNotConfirm(t *testing.T) {
	t.Parallel()
	for _, text := range []string{`He said "yes indeed" yesterday`, `He said "absolutely yes" yesterday`, `He said "yes" yesterday`, `He said 'yes' yesterday`, "Forwarded:\nyes", `"yes"`, "“yes”", "> yes", "Forwarded: yes", "```yes```"} {
		t.Run(text, func(t *testing.T) {
			start := strings.Index(text, "yes")
			request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "current", Text: text}}, Proposal: "proposal"}
			raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "confirm", Proposal: "proposal", Confirmation: &ConversationSpan{SourceID: "current", Start: start, End: start + 3}})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := DecodeConversationEnvelope(raw, request); err == nil {
				t.Fatal("quoted or forwarded data granted confirmation")
			}
		})
	}
	for _, span := range []ConversationSpan{{Start: 1, End: 2}, {Start: 0, End: 1}, {Start: -1, End: 2}, {Start: 0, End: 3}} {
		if _, ok := ownerArgumentSpan("é", span); ok {
			t.Fatalf("invalid UTF-8 span accepted: %+v", span)
		}
	}
}

package messaging

import (
	"strings"
	"testing"
)

func TestConversationProseFallback(t *testing.T) {
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "owner", Text: "Explain the current limits."}}}
	text := "You can ask for help in ordinary words.\nQuoted words such as ‘yes’ do not approve an action."
	envelope, invocation, err := DecodeConversationEnvelope([]byte(text), request)
	if err != nil || envelope.Kind != "answer" || envelope.Text != text || invocation.Command != "" {
		t.Fatalf("fallback: %+v %+v %v", envelope, invocation, err)
	}
	for _, raw := range []string{
		`{"version":1,"kind":"propose","command":"draft"`,
		`kind: confirm`,
		`{"kind":"propose", damaged {"version":1,"kind":"answer","text":"nested"}}`,
		`leading {damaged} {"version":1,"kind":"answer","text":"nested"}`,
		`[{"version":1,"kind":"answer","text":"nested"}]`,
		`{"version":1,"kind":"answer","text":"first","text":"second"}`,
		`{"version":1,"kind":"answer","text":"a","extra":true}`,
		`{"version":1,"kind":"answer","text":"a"} {"version":1,"kind":"answer","text":"b"}`,
		"```json\nversion: 1\n```",
		`["answer"]`,
		strings.Repeat("a", 32769),
		"bad\x00text",
		string([]byte{0xff}),
	} {
		if _, _, err := DecodeConversationEnvelope([]byte(raw), request); err == nil {
			t.Fatalf("accepted malformed or oversized output: %q", raw)
		}
	}
}

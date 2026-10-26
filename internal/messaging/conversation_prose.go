package messaging

import (
	"bytes"
	"regexp"
)

// A damaged protocol payload needs correction, even when it contains readable
// words. In particular, never downgrade a failed action envelope into an answer.
var conversationProtocolFragment = regexp.MustCompile(`(?im)(?:["']?(?:version|kind|command|arguments|proposal|confirmation|blocks|fact_refs|source_id)["']?\s*[:=]|^\s*(?:lookup|propose|confirm|cancel|consult)\s*[:(]|\x60\x60\x60\s*(?:json|javascript)\b)`)

func safeConversationProse(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.ContainsAny(trimmed, "{}") || conversationProtocolFragment.Match(trimmed) {
		return false
	}
	// Arrays, string literals and protocol-like XML are structured output, not
	// conversational prose. Markdown lists and inline quoted words remain prose.
	return trimmed[0] != '[' && trimmed[0] != '"' && trimmed[0] != '<'
}

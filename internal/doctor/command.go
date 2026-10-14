package doctor

import (
	"strings"
	"unicode"

	"github.com/cloudvane/litefactory/internal/commandtext"
)

// Command is a doctor envelope, not a workflow mode or repair authority.
type Command struct {
	Operation string
	Text      string
}

// ParseCommand recognizes only a complete first token, ignoring leading space
// and case. Quoted, embedded and fenced occurrences retain ordinary meaning.
func ParseCommand(body string) (Command, bool) {
	body = strings.TrimSpace(commandtext.Normalize(body))
	token, rest := body, ""
	if i := strings.IndexFunc(body, unicode.IsSpace); i >= 0 {
		token, rest = body[:i], strings.TrimSpace(body[i:])
	}
	if !strings.EqualFold(token, "/doctor") {
		return Command{}, false
	}
	if rest == "" {
		return Command{Operation: "usage"}, true
	}
	word := rest
	if i := strings.IndexFunc(rest, unicode.IsSpace); i >= 0 {
		word = rest[:i]
	}
	switch strings.ToLower(word) {
	case "stop", "apply":
		if !strings.EqualFold(word, rest) {
			return Command{Operation: "usage"}, true
		}
		return Command{Operation: strings.ToLower(word)}, true
	default:
		if len(rest) > 32768 {
			return Command{Operation: "usage"}, true
		}
		return Command{Operation: "diagnose", Text: rest}, true
	}
}

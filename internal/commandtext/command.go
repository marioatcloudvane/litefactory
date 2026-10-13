package commandtext

import (
	"strings"
	"unicode"
)

// Normalize unwraps one single-backtick span occupying the entire first
// nonblank line, only when it begins with a slash-command token. It preserves
// arguments, leading whitespace and following lines; it never scans prose.
func Normalize(body string) string {
	start := len(body) - len(strings.TrimLeftFunc(body, unicode.IsSpace))
	line := body[start:]
	if end := strings.IndexAny(line, "\r\n"); end >= 0 {
		line = line[:end]
	}
	line = strings.TrimRightFunc(line, unicode.IsSpace)
	if len(line) < 3 || line[0] != '`' || line[len(line)-1] != '`' {
		return body
	}
	inner := line[1 : len(line)-1]
	if strings.ContainsRune(inner, '`') {
		return body
	}
	token := inner
	if end := strings.IndexFunc(token, unicode.IsSpace); end >= 0 {
		token = token[:end]
	}
	if len(token) < 2 || token[0] != '/' {
		return body
	}
	for i, r := range token[1:] {
		alphanumeric := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		interiorPunctuation := i > 0 && (r == '-' || r == '_')
		if !alphanumeric && !interiorPunctuation {
			return body
		}
	}
	return body[:start] + inner + body[start+len(line):]
}

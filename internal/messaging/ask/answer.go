package ask

import (
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/redact"
)

// RenderOptions carries only ephemeral turn metadata and a mandatory redactor.
type RenderOptions struct {
	Question           string
	InheritedProject   bool
	HistoryUnavailable bool
	OlderContextCut    bool
	Redactor           *redact.Redactor
}

func runeCount(s string) int { return len(utf16.Encode([]rune(s))) }

func runeExcerpt(s string, limit int) string {
	runes := []rune(s)
	if runeCount(s) <= limit {
		return s
	}
	if limit < 1 {
		return ""
	}
	units, end := 0, 0
	for end < len(runes) && units+utf16.RuneLen(runes[end]) <= limit-1 {
		units += utf16.RuneLen(runes[end])
		end++
	}
	return string(runes[:end]) + "…"
}

package presentation

import (
	"strings"
	"unicode/utf16"
)

// MessageLimit is Discord's visible message budget in UTF-16 units.
const MessageLimit = 2000

// FallbackNotice is the only visible text of a raw fallback. The raw result
// itself is delivered once, in the same-message attachment.
const FallbackNotice = "Couldn't phrase this one, raw result attached."

// Render checks both deterministic validation and independent support before
// showing the checked prose. The raw result is delivered only in the
// attachment. Any rejected or oversized candidate selects the raw fallback.
// The delivery caller must still recheck redaction before persisting a payload.
func Render(o Outcome, candidate Candidate, check Check) (Payload, State, error) {
	if err := o.Validate(); err != nil {
		return Payload{}, "", err
	}
	if candidate.Validate(o) != nil || check.Validate(candidate) != nil {
		payload, err := Raw(o)
		return payload, RawFallback, err
	}
	prose := make([]string, 0, len(candidate.Claims))
	for _, claim := range candidate.Claims {
		prose = append(prose, claim.Text)
	}
	content := strings.Join(prose, "\n")
	if messageSize(content) > MessageLimit {
		payload, err := Raw(o)
		return payload, RawFallback, err
	}
	return Payload{Content: content, Attachment: &Attachment{Name: RawFilename, Content: o.RawText}}, Narrated, nil
}

// Raw keeps the original result available verbatim in the same-message
// attachment and shows only a one-line notice.
func Raw(o Outcome) (Payload, error) {
	if err := o.Validate(); err != nil {
		return Payload{}, err
	}
	return Payload{Content: FallbackNotice, Attachment: &Attachment{Name: RawFilename, Content: o.RawText}}, nil
}

func messageSize(text string) int {
	return len(utf16.Encode([]rune(text)))
}

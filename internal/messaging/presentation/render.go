package presentation

import (
	"errors"
	"strings"
	"unicode/utf16"
)

// MessageLimit is Discord's visible message budget in UTF-16 units.
const MessageLimit = 2000

// Render checks both deterministic validation and independent support before
// assembling prose with untouched protected source fields. Any rejected or
// oversized candidate selects the existing raw result, not truncated tokens.
// The delivery caller must still recheck redaction before persisting a payload.
func Render(o Outcome, candidate Candidate, check Check) (Payload, State, error) {
	if err := o.Validate(); err != nil {
		return Payload{}, "", err
	}
	if candidate.Validate(o) != nil || check.Validate(candidate) != nil {
		payload, err := Raw(o)
		return payload, RawFallback, err
	}
	var prefixes, fixed, prose []string
	for _, field := range o.Protected {
		if field.Kind == AskPrefix {
			prefixes = append(prefixes, field.Source.Value)
		} else {
			fixed = append(fixed, field.Source.Value)
		}
	}
	for _, fact := range o.Facts {
		if fact.Mandatory {
			fixed = append(fixed, fact.Source.Value)
		}
	}
	for _, claim := range candidate.Claims {
		prose = append(prose, claim.Text)
	}
	content := strings.Join(append(append(prefixes, prose...), fixed...), "\n")
	if messageSize(content) > MessageLimit {
		payload, err := Raw(o)
		return payload, RawFallback, err
	}
	return Payload{Content: content, Attachment: &Attachment{Name: RawFilename, Content: o.RawText}}, Narrated, nil
}

// Raw keeps the original result available verbatim in the same-message
// attachment. When an inline result fits, its bytes are also shown unchanged.
func Raw(o Outcome) (Payload, error) {
	if err := o.Validate(); err != nil {
		return Payload{}, err
	}
	content := o.RawText + "\n\nRaw result."
	if messageSize(content) > MessageLimit {
		var prefix string
		for _, field := range o.Protected {
			if field.Kind == AskPrefix {
				prefix = field.Source.Value + "\n"
			}
		}
		content = prefix + "Raw result attached."
		if messageSize(content) > MessageLimit {
			return Payload{}, errors.New("presentation: protected prefix exceeds message budget")
		}
	}
	return Payload{Content: content, Attachment: &Attachment{Name: RawFilename, Content: o.RawText}}, nil
}

func messageSize(text string) int {
	return len(utf16.Encode([]rune(text)))
}

package confirm

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidRequest means a confirmation control did not match the
// supported grammar. Callers must refuse it instead of treating any part of
// it as agent text or as a valid confirmation (S-14).
var ErrInvalidRequest = errors.New("invalid confirm request")

// Phrase is the exact phrase an owner must type, verbatim (case-insensitive),
// together with a proposal id, to apply a stored conflict-resolution
// proposal (US-12, S-14, watch-out #5): neither "yes" nor a reaction is
// consent — the id and this phrase are both required, and nothing else is
// accepted in its place.
const Phrase = "apply this resolution"

// charset is every rune [Parse] accepts in the phrase position, once
// lower-cased: plain letters and single interior spaces. Restricting it
// before comparing against [Phrase] is what keeps a malformed or padded
// attempt at the phrase from being folded into a match by a looser
// comparison, and keeps this package's own error messages free of whatever
// bytes a comment actually contained.
const charset = "abcdefghijklmnopqrstuvwxyz "

// Request is the validated, factory-owned representation of one confirmation
// control: the proposal id it names. Ownership — whether the commenter who
// wrote it may act for this session — is checked separately, after parsing
// succeeds; a Request says only that the grammar and the phrase matched.
type Request struct {
	ProposalID int64
}

// Parse parses the text after the confirmation command token: a positive
// proposal id, then [Phrase], case-insensitively, and nothing else.
//
// Matching is deliberately narrow rather than forgiving: unlike
// [ParseCommand]'s tolerance for phone-keyboard capitalisation of a command
// word, a confirmation authorises a write to a repository, so an owner is
// asked to type the phrase rather than have a near-miss silently accepted.
// A body that does not match — a missing id, extra words, punctuation, a
// different phrase entirely — refuses in full; there is no partial credit
// and no salvage of "the part that looked right" (S-14).
func Parse(text string) (Request, error) {
	idText, rest := next(text)
	id, err := positiveID(idText)
	if err != nil {
		return Request{}, err
	}

	phrase := strings.ToLower(strings.TrimSpace(rest))
	if phrase == "" {
		return Request{}, invalid("a confirmation phrase is required")
	}
	for _, r := range phrase {
		if !strings.ContainsRune(charset, r) {
			return Request{}, invalid("the confirmation phrase must use plain lowercase letters and spaces only")
		}
	}
	if phrase != Phrase {
		return Request{}, invalid("expected the confirmation phrase %q", Phrase)
	}
	return Request{ProposalID: id}, nil
}

func positiveID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("a positive proposal id is required")
	}
	return id, nil
}

// next splits off the first whitespace-delimited token of s and returns it
// with everything after it, both trimmed.
func next(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	i := strings.IndexAny(s, " \t\r\n")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

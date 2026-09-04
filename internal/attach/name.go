package attach

import (
	"fmt"
	"strings"
)

// maxFileNameStemLen bounds the descriptive part of a minted filename. The
// stem is a convenience for whoever reads the landing directory later; the
// ordinal in front of it is what makes the name unique, so there is no
// reason to let a 4KB alt text become a 4KB filename.
const maxFileNameStemLen = 40

// mintFileName builds the name an attachment lands under: a one-based
// ordinal, a slug salvaged from what the body called it, and the extension
// for the type its bytes actually sniffed as.
//
// The name is litefactory's, not the uploader's. Nothing the forge supplied
// survives into it except as lowercase ASCII letters, digits and single
// dashes, and the ordinal in front guarantees uniqueness within the run's
// directory even when two attachments slug to the same thing. That is what
// makes this safe to join onto a directory path: there is no separator, no
// "..", no leading dot, no NUL, and no empty result to collapse the join.
//
// The extension comes from [sniff], never from the declared name, so the
// file on disk announces what it really is (AC-3.5).
func mintFileName(ordinal int, name, ext string) string {
	stem := slugify(name)
	if stem == "" {
		stem = "attachment"
	}
	return fmt.Sprintf("%02d-%s%s", ordinal, stem, ext)
}

// slugify reduces third-party text to lowercase ASCII letters, digits and
// single dashes, with no leading or trailing dash.
//
// Every rune outside that set becomes a dash rather than being dropped,
// then runs of dashes collapse. Substitution keeps two different names from
// colliding the way deletion can — "a/b" and "ab" stay distinct — which
// matters because the result is part of a filename.
//
// Non-ASCII is deliberately not transliterated. A name in a script this
// function cannot romanise slugs to nothing and falls back to the generic
// stem, which is honest; a mangled transliteration would look like
// information while being none.
func slugify(name string) string {
	var b strings.Builder
	lastDash := true // leading dashes are suppressed
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if b.Len() >= maxFileNameStemLen {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

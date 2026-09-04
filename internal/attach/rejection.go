package attach

import (
	"fmt"
	"strings"
)

// maxAttachmentNameLen bounds how much of a third-party-controlled
// attachment name attachmentNameSpan will echo. It is generous enough for
// any real filename and short enough that a pathological one cannot bloat a
// rejection line.
const maxAttachmentNameLen = 80

// Rejection is one attachment litefactory declined to ingest: the shared
// shape used both to compose the rejection's reply line ([Rejection.Line])
// and to record it in the run's attachment manifest.
//
// This story (US-6) defines the shape only. The manifest file itself —
// attachments.md, its format, and the code that writes it — is a later
// story's job; this type is what that code will range over to render one
// row per rejection, and what an ingestion path constructs one of every
// time it turns an attachment away.
type Rejection struct {
	// Name is the attachment's filename as declared by the forge attachment
	// metadata. It is third-party-controlled, so it is sanitized before it
	// is ever echoed anywhere — see [Rejection.Line].
	Name string

	// Reason is litefactory's own classification of why the attachment was
	// declined. Safe to state directly.
	Reason RejectionReason

	// Detail is additional, litefactory-composed context (for example, a
	// byte count measured against a configured ceiling). It is never
	// populated from raw attachment content, a fetch URL, or the
	// installation token — callers must keep it that way for AC-6.5 to
	// hold, the same contract this package holds for every other field.
	Detail string
}

// NewRejection builds a [Rejection], validating that it has a name and a
// real [RejectionReason]. The returned error, when non-nil, wraps
// [ErrInvalidRejection] and signals a caller mistake, not an attachment that
// failed to qualify — see the package doc comment.
func NewRejection(name string, reason RejectionReason, detail string) (Rejection, error) {
	if name == "" {
		return Rejection{}, fmt.Errorf("%w: empty attachment name", ErrInvalidRejection)
	}
	if !reason.valid() {
		return Rejection{}, fmt.Errorf("%w: reason %d is not a recognized rejection reason", ErrInvalidRejection, int(reason))
	}
	return Rejection{Name: name, Reason: reason, Detail: detail}, nil
}

// Line renders a single, safe summary line for the rejection: the sanitized
// attachment name followed by the reason-only sentence fragment. It never
// contains a URL, byte content, base64, or a bearer-bearing query string
// (AC-6.5) — Rejection carries no field that could supply one, and the name
// is sanitized on the way out regardless.
func (rj Rejection) Line() string {
	name, named := attachmentNameSpan(rj.Name)
	if !named {
		name = "An attachment"
	}
	return name + " " + rj.Reason.ReasonText() + "."
}

// Lines renders one safe [Rejection.Line] per rejection, in the order
// given, so a caller can report every attachment a turn dropped in one
// place (AC-6.3). Empty input produces no lines.
//
// This is a pure, freestanding composer: it does not decide where the
// lines are posted, and it does not itself pass through the redactor —
// whatever assembles the comment that eventually carries these lines is the
// one place a body is posted, and every body still passes through
// internal/redact there before it goes out, the same as every other reply
// and run report.
func Lines(rejections []Rejection) []string {
	lines := make([]string, 0, len(rejections))
	for _, rj := range rejections {
		lines = append(lines, rj.Line())
	}
	return lines
}

// attachmentNameSpan salvages a safe, printable prefix of name suitable for
// echoing into a comment, wrapped in backticks as an inline code span.
//
// name is third-party-controlled (the forge's attachment metadata), so this
// mirrors internal/report's practice of re-validating forge-supplied text
// immediately before it is rendered rather than trusting whatever produced
// it upstream. It salvages a prefix — stopping at the first disallowed rune
// — rather than dropping disallowed runes and continuing, the same choice
// internal/report's own command-token sanitizer makes: reassembling the
// runes on either side of a dropped one could produce a token that never
// appeared in the input, which is worse than truncating. Control characters,
// backticks (which would break out of the span), angle brackets (markup),
// and '?' (the start of a query string, never legitimate in a bare
// filename, and one more layer of defense for AC-6.5 in case a caller ever
// mistakenly passes something URL-shaped as Name) all end the salvage.
//
// It reports false when nothing safe remains, so the caller can fall back
// to a generic phrase rather than emit an empty span.
func attachmentNameSpan(name string) (string, bool) {
	var b strings.Builder
	for _, r := range name {
		safe := r >= 0x20 && r != 0x7f && r != '`' && r != '<' && r != '>' && r != '?'
		if !safe {
			break
		}
		if b.Len() >= maxAttachmentNameLen {
			break
		}
		b.WriteRune(r)
	}
	token := strings.TrimSpace(b.String())
	if token == "" {
		return "", false
	}
	return "`" + token + "`", true
}

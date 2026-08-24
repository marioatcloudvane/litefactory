package redact

// Body is a comment body that has been through the S-7 redaction pass.
//
// # Why this type exists
//
// It is the compile-time half of the guarantee "no credential ever appears in a
// public GitHub comment". Every field is unexported, there is no exported
// constructor and no exported field to fill in, so a composite literal written
// in another package produces the zero value and nothing else. The only way to
// obtain a Body carrying text is [Redactor.Apply].
//
// github.PostComment takes a Body and never a string. Together those two facts
// mean that posting an unredacted body is not something you can write —
// not a rule a reviewer has to catch, not a lint someone can silence. Making
// that mistake requires editing this file first.
//
// # The zero value
//
// A Body zero value carries no text and reports Valid() == false. It is empty
// rather than dangerous: there is no way to get arbitrary text into a Body
// without passing it through Apply, so the worst a `redact.Body{}` can do is
// try to post an empty comment. Valid() exists so the poster can refuse it with
// a clear error instead of sending an empty body to the API.
//
// # Do not add
//
// No Unsafe constructor, no NewBody, no exported Text field, no
// UnmarshalJSON. Every one of those reopens the hole this type closes. If a
// test needs a Body, it calls Apply — which is cheap, deterministic, and
// exercises the thing under test anyway.
type Body struct {
	// text is the redacted, size-bounded body.
	text string
	// counts is the per-class substitution tally from the pass that produced
	// this body.
	counts Counts
	// truncated records that Apply removed text to fit the size limit.
	truncated bool
	// valid distinguishes a Body produced by Apply from the zero value. It is
	// belt-and-braces alongside the empty text: an explicit flag makes the
	// poster's refusal unambiguous even for a legitimately empty body.
	valid bool
}

// String returns the redacted body. It is safe to send to a forge; that is the
// entire postcondition of this type.
func (b Body) String() string { return b.text }

// Len returns the size of the body in bytes, which is the unit forge comment
// limits are expressed in.
func (b Body) Len() int { return len(b.text) }

// Valid reports whether this Body came out of [Redactor.Apply]. A zero
// `redact.Body{}` reports false; the poster should refuse it rather than send
// an empty comment.
func (b Body) Valid() bool { return b.valid }

// Truncated reports whether [Redactor.Apply] removed text to fit the size
// limit.
func (b Body) Truncated() bool { return b.truncated }

// Counts returns the per-class substitution tally. It carries integers keyed by
// [Class] only — never matched text — so it is safe to log.
func (b Body) Counts() Counts { return b.counts }

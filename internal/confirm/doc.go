// Package confirm parses Litefactory-owned confirmation controls (US-12).
//
// It has no clock, storage, forge, or agent dependency, the same as
// internal/schedule: it is a pure function from a comment's text to a
// validated request. The ownership check — is the commenter the issue
// owner? — is not this package's job and is applied after parsing, by
// internal/turn's confirmation gate; this package only says whether the
// text is the confirmation grammar at all.
package confirm

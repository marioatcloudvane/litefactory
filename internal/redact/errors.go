package redact

import (
	"errors"
	"fmt"
)

// ErrRedactionFailed marks every condition under which [Redactor.Apply] refuses
// to produce a [Body]. A caller matches on it to decide "do not post, do not
// advance the watermark, leave the run un-reported" without caring which
// specific thing went wrong.
var ErrRedactionFailed = errors.New("redaction failed")

// ErrUninitialized reports a [Redactor] this package did not build. A
// `redact.Redactor{}` composite literal compiles, and a zero Redactor knows no
// literals — it would happily pass the bot token straight through. It refuses
// to run instead, the same way a zero git.Clone refuses to run git.
var ErrUninitialized = fmt.Errorf("%w: redactor was not produced by redact.New", ErrRedactionFailed)

// ErrInvalidLimit reports a non-positive size limit. There is no "unlimited"
// mode: every forge has a comment size limit, and a caller that does not know
// it is a caller that has not thought about AC-4.8. It wraps
// [ErrRedactionFailed], so the one check a caller has to remember is that one.
var ErrInvalidLimit = fmt.Errorf("%w: comment size limit must be positive", ErrRedactionFailed)

// PanicError reports that the redaction pass panicked.
//
// This is the fail-closed path (AC-4.9). A panic means the pass did not finish,
// so the body must be assumed to still contain a credential and the post is
// abandoned.
//
// The recovered value is rendered into Value here, not carried as an interface,
// so that this error cannot smuggle a live credential into whatever formats it.
// The value and the stack reach daemon.log, which is local-only and unredacted
// by design (S-7); they never reach a forge, because on this path nothing is
// posted.
type PanicError struct {
	// Value is the recovered panic value, already formatted.
	Value string
	// Type is the dynamic type of the recovered value.
	Type string
	// Stack is the goroutine stack captured at the point of recovery.
	Stack string
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("redaction pass panicked (%s): %s", e.Type, e.Value)
}

func (e *PanicError) Unwrap() error { return ErrRedactionFailed }

// ResidualError reports that a credential this package knows verbatim survived
// the redaction pass.
//
// It cannot happen unless the pass has a bug, which is exactly why it is
// checked: pattern coverage is a judgement call, but "is the bot token in the
// outbound body" is a fact, and it is cheap to establish before posting.
type ResidualError struct {
	// Class is the literal class that survived — never the value.
	Class Class
}

func (e *ResidualError) Error() string {
	return fmt.Sprintf("redaction pass left a %s in the body", e.Class)
}

func (e *ResidualError) Unwrap() error { return ErrRedactionFailed }

// LimitError reports a limit that [Redactor.Apply] could not honour.
type LimitError struct {
	// Limit is the requested maximum body size in bytes.
	Limit int
	// Err is the underlying condition.
	Err error
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("comment size limit %d: %v", e.Limit, e.Err)
}

func (e *LimitError) Unwrap() error { return e.Err }

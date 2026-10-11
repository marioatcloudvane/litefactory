package secrets

import (
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
)

// Redacted is the fixed replacement litefactory prints instead of a
// credential. It is the same literal internal/redact substitutes into outbound
// bodies, so a reader sees one marker everywhere.
const Redacted = "[redacted]"

// Token is a resolved credential.
//
// The value is unexported and every formatting path is overridden, so a token
// cannot leak by accident (AC-0.11):
//
//   - fmt.Formatter covers every verb, so %v, %s, %q, %d and %#v all print
//     [redacted] — including a Token nested inside some other struct being
//     printed with %+v.
//   - slog.LogValuer covers slog.Any and a Token passed as an attribute value.
//   - MarshalText and MarshalJSON cover the JSON log handler and any structure
//     that gets serialised on its way into a file or an API call.
//
// Reading the real value takes an explicit [Token.Reveal] call, which is
// greppable. Factory transport boundaries reveal credentials only to authorize
// GitHub or Linear requests and configure webhook signing. internal/redact
// registers the literal value for outbound hygiene; tests may inspect it.
//
// A token is resolved once per loaded immutable connection version, and it is never passed to a subprocess through argv, where it
// would be visible in ps output to every user on the machine.
type Token struct {
	value string
}

// Compile-time proof that every formatting path is covered. Deleting one of
// these methods breaks the build here rather than leaking a token in
// production.
var (
	_ fmt.Stringer                 = Token{}
	_ fmt.Formatter                = Token{}
	_ fmt.GoStringer               = Token{}
	_ slog.LogValuer               = Token{}
	_ encoding.TextMarshaler       = Token{}
	_ json.Marshaler               = Token{}
	_ encoding.TextMarshaler       = &Token{}
	_ fmt.Stringer                 = &Token{}
	_ slog.LogValuer               = &Token{}
	_ json.Marshaler               = &Token{}
	_ interface{ Reveal() string } = Token{}
)

// NewToken wraps a credential value.
func NewToken(value string) Token { return Token{value: value} }

// Reveal returns the credential itself. Every call site is a deliberate
// decision to handle the raw value.
func (t Token) Reveal() string { return t.value }

// Empty reports whether the token carries no value.
func (t Token) Empty() bool { return t.value == "" }

// String implements fmt.Stringer.
func (t Token) String() string { return Redacted }

// GoString implements fmt.GoStringer, covering %#v for callers that reach for
// it directly rather than through fmt.
func (t Token) GoString() string { return Redacted }

// Format implements fmt.Formatter. It ignores the verb on purpose: a token
// printed with a numeric or a struct verb must still be redacted, and
// fmt.Stringer alone would not catch %d or a Token embedded in a struct
// printed with %+v.
func (t Token) Format(state fmt.State, verb rune) {
	out := Redacted
	if verb == 'q' {
		out = strconv.Quote(Redacted)
	}
	_, _ = io.WriteString(state, out)
}

// LogValue implements slog.LogValuer, so slog.Any("token", tok) logs the
// marker and never the value.
func (t Token) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalText implements encoding.TextMarshaler.
func (t Token) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// MarshalJSON implements json.Marshaler, which is the path the JSON log
// handler and any metadata writer take.
func (t Token) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

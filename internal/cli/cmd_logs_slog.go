package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"
)

// Turning daemon.log's JSON back into what slog.TextHandler would have
// printed.
//
// daemon.log is written by internal/logging with slog.NewJSONHandler, and the
// console (when it is a terminal) gets the same records from
// slog.NewTextHandler. This sink reverses that: it re-parses a daemon.log
// line's JSON and renders it exactly as the TextHandler sibling would have,
// so `litefactory logs` on the daemon's own log reads the same as watching
// `litefactory daemon run` scroll by. No new format to learn.
//
// This is only possible because internal/logging.New configures neither
// AddSource nor ReplaceAttr on its handlers (see logging.go). AddSource would
// add a "source" key the JSON side doesn't special-case here; ReplaceAttr
// could rewrite or drop keys in a way this renderer has no way to observe
// from the JSON alone. If either is ever added to internal/logging, this
// renderer silently stops being byte-identical — there is nothing in the
// on-disk format that would tell it so.
//
// The decode is order-preserving (built on json.Decoder.Token, never
// map[string]any, which would destroy attribute order) because TextHandler's
// output order is the order attributes were logged in, and that is exactly
// the key order JSONHandler wrote them in.
//
// A JSON round-trip cannot always be byte-identical: slog.Duration and
// slog.Int64 both encode as a bare JSON number, so a Duration attr renders
// here as a plain integer rather than as "1.5s". This is an accepted,
// inherent limitation of decoding from JSON alone with no shared Go type
// between writer and reader (see the cross-story constraint against adding
// one) — the sink still never drops or errors on such a line.

// slogTextSink re-renders each daemon.log line as slog.TextHandler text. It
// implements logSink so it can sit alongside rawSink and renderSink as a
// selectable pipeline stage.
type slogTextSink struct {
	out io.Writer
}

// newSlogTextSink builds a sink writing TextHandler-equivalent lines to out.
func newSlogTextSink(out io.Writer) *slogTextSink {
	return &slogTextSink{out: out}
}

// write renders one daemon.log line and writes it, followed by a newline.
//
// done is always false: unlike a run log, the app log has no terminal event —
// the daemon just keeps logging until it exits, so there is nothing for this
// sink to recognize as "the end".
func (s *slogTextSink) write(line []byte) (bool, error) {
	var out string
	if rendered, ok := renderSlogLine(line); ok {
		out = rendered + "\n"
	} else {
		// Not valid JSON, or valid JSON that isn't an object: pass it through
		// exactly as read — including whatever trailing newline (or lack of
		// one, for a truncated final line) it already had — rather than drop
		// it or fail the whole view.
		out = string(line)
	}
	if _, err := io.WriteString(s.out, out); err != nil {
		return false, fmt.Errorf("write daemon log: %w", err)
	}
	return false, nil
}

// renderSlogLine decodes a single daemon.log JSON line and renders it as
// slog.TextHandler would have. It reports ok=false for anything that isn't a
// single JSON object occupying the whole line, in which case the line must be
// passed through unrendered rather than dropped.
func renderSlogLine(line []byte) (string, bool) {
	obj, ok := decodeJSONObjectLine(line)
	if !ok {
		return "", false
	}
	tokens := renderSlogFields(obj, "")
	out := ""
	for i, tok := range tokens {
		if i > 0 {
			out += " "
		}
		out += tok
	}
	return out, true
}

// jsonFieldKind is the JSON token kind of a decoded value, as needed to
// reproduce TextHandler's per-kind rendering.
type jsonFieldKind int

const (
	jsonKindString jsonFieldKind = iota
	jsonKindNumber
	jsonKindBool
	jsonKindNull
	jsonKindObject
	jsonKindOther // array, or anything else: rendered as its compact JSON text
)

// jsonValue is one decoded JSON value, keeping enough of the original to
// render it the way TextHandler would render the slog.Value it came from.
type jsonValue struct {
	kind jsonFieldKind
	str  string      // string content, or the number's literal token text
	b    bool        // for jsonKindBool
	obj  []jsonField // for jsonKindObject
	raw  string      // for jsonKindOther: the value's compact JSON text
}

// jsonField is one key/value pair of a decoded JSON object, in the order it
// was written.
type jsonField struct {
	key string
	val jsonValue
}

// decodeJSONObjectLine decodes line as exactly one JSON object with no
// trailing data, preserving key order. It reports ok=false for anything else
// (invalid JSON, a JSON value that isn't an object, or trailing bytes after
// the object) — the caller must treat that as "pass through verbatim".
func decodeJSONObjectLine(line []byte) ([]jsonField, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, false
	}
	obj, err := decodeJSONObjectBody(dec)
	if err != nil {
		return nil, false
	}
	// The line must not contain anything besides this one object.
	if dec.More() {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return obj, true
}

// decodeJSONObjectBody decodes the fields of a JSON object whose opening '{'
// has already been consumed, up to and including the closing '}'.
func decodeJSONObjectBody(dec *json.Decoder) ([]jsonField, error) {
	var fields []jsonField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string")
		}
		val, err := decodeJSONValue(dec)
		if err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{key: key, val: val})
	}
	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return fields, nil
}

// decodeJSONValue decodes one JSON value at the decoder's current position.
func decodeJSONValue(dec *json.Decoder) (jsonValue, error) {
	tok, err := dec.Token()
	if err != nil {
		return jsonValue{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj, err := decodeJSONObjectBody(dec)
			if err != nil {
				return jsonValue{}, err
			}
			return jsonValue{kind: jsonKindObject, obj: obj}, nil
		case '[':
			raw, err := skipJSONArray(dec)
			if err != nil {
				return jsonValue{}, err
			}
			return jsonValue{kind: jsonKindOther, raw: raw}, nil
		default:
			return jsonValue{}, fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		return jsonValue{kind: jsonKindString, str: t}, nil
	case json.Number:
		return jsonValue{kind: jsonKindNumber, str: t.String()}, nil
	case bool:
		return jsonValue{kind: jsonKindBool, b: t}, nil
	case nil:
		return jsonValue{kind: jsonKindNull}, nil
	default:
		return jsonValue{}, fmt.Errorf("unexpected token %v", tok)
	}
}

// skipJSONArray consumes a JSON array whose opening '[' has already been
// read, and returns its compact JSON text. Arrays are not a shape any
// built-in slog Kind decodes to on its own, so there is no per-kind text form
// to reconstruct for one; the compact JSON text is the closest available
// rendering and is only ever used as a last resort (see jsonKindOther).
func skipJSONArray(dec *json.Decoder) (string, error) {
	var elems []jsonValue
	for dec.More() {
		v, err := decodeJSONValue(dec)
		if err != nil {
			return "", err
		}
		elems = append(elems, v)
	}
	if _, err := dec.Token(); err != nil { // closing ']'
		return "", err
	}
	parts := make([]string, len(elems))
	for i, v := range elems {
		parts[i] = compactJSONText(v)
	}
	out := "["
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out + "]", nil
}

// compactJSONText renders v back to compact JSON text, for use inside
// skipJSONArray's fallback rendering.
func compactJSONText(v jsonValue) string {
	switch v.kind {
	case jsonKindString:
		b, _ := json.Marshal(v.str)
		return string(b)
	case jsonKindNumber:
		return v.str
	case jsonKindBool:
		return strconv.FormatBool(v.b)
	case jsonKindNull:
		return "null"
	case jsonKindObject:
		out := "{"
		for i, f := range v.obj {
			if i > 0 {
				out += ","
			}
			keyJSON, _ := json.Marshal(f.key)
			out += string(keyJSON) + ":" + compactJSONText(f.val)
		}
		return out + "}"
	default: // jsonKindOther
		return v.raw
	}
}

// renderSlogFields renders a decoded object's fields as TextHandler
// key=value tokens, in the order the JSON had them. prefix is the dotted
// group path accumulated from any enclosing WithGroup objects.
//
// The very first field of the top-level object gets special treatment: if it
// is named "time" and holds a JSON string, it is the built-in time attribute
// slog.commonHandler.handle always writes first, and its timestamp is
// reformatted from JSON's RFC3339-nanosecond form to TextHandler's
// RFC3339-millisecond form. A field named "time" anywhere else is an ordinary
// attribute and is rendered as a plain string instead — the built-in key is
// only ever in that first position.
func renderSlogFields(fields []jsonField, prefix string) []string {
	var toks []string
	for i, f := range fields {
		if i == 0 && prefix == "" && f.key == "time" && f.val.kind == jsonKindString {
			if ts, ok := reformatRFC3339Millis(f.val.str); ok {
				toks = append(toks, "time="+ts)
				continue
			}
		}
		toks = append(toks, renderSlogField(f, prefix)...)
	}
	return toks
}

// renderSlogField renders one field, recursing into nested objects (which
// are how a WithGroup group round-trips through JSON) and flattening them to
// dotted keys. An empty group produces no tokens at all, matching
// TextHandler eliding groups with no attributes.
func renderSlogField(f jsonField, prefix string) []string {
	if f.val.kind == jsonKindObject {
		if len(f.val.obj) == 0 {
			return nil
		}
		childPrefix := prefix
		if f.key != "" {
			childPrefix = prefix + f.key + "."
		}
		inner := renderSlogFields(f.val.obj, childPrefix)
		if len(inner) == 0 {
			return nil
		}
		return inner
	}
	return []string{renderKeyValue(prefix+f.key, f.val)}
}

// renderKeyValue renders one non-group field as TextHandler's "key=value",
// including TextHandler's quoting rules for both the key and the value.
func renderKeyValue(key string, val jsonValue) string {
	return quoteSlogString(key) + "=" + renderSlogValue(val)
}

// renderSlogValue renders a single JSON value the way TextHandler renders
// the slog.Value it was encoded from.
func renderSlogValue(v jsonValue) string {
	switch v.kind {
	case jsonKindString:
		return quoteSlogString(v.str)
	case jsonKindNumber:
		// Both integer and floating-point slog Kinds write plain decimal
		// text in TextHandler, identical to what JSONHandler wrote for the
		// same value, so the literal token text carries over unchanged. No
		// digit of a JSON number ever needs quoting.
		return v.str
	case jsonKindBool:
		return strconv.FormatBool(v.b)
	case jsonKindNull:
		// The most common source of a bare JSON null is slog.Any(key, nil),
		// which TextHandler renders as "<nil>" (fmt.Sprintf("%+v", nil)).
		return "<nil>"
	case jsonKindObject:
		// Reached only for a group nested as a value where renderSlogField
		// didn't already flatten it away, which appendSlogField prevents;
		// kept as a safe fallback rather than a panic.
		return quoteSlogString(compactJSONText(v))
	default: // jsonKindOther
		return quoteSlogString(v.raw)
	}
}

// reformatRFC3339Millis parses s as RFC3339Nano (the form JSONHandler writes
// for a time.Time) and reformats it the way slog's appendRFC3339Millis does:
// truncated to millisecond resolution, with exactly three fractional digits.
// It reports ok=false if s does not parse as a timestamp, in which case the
// field must be rendered as an ordinary string instead.
func reformatRFC3339Millis(s string) (string, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "", false
	}
	// Mirrors slog's appendRFC3339Millis: format with RFC3339Nano (which
	// trims trailing zero fractional digits) after nudging the time forward
	// by a tenth of a millisecond, then drop the resulting 4th fractional
	// digit, which is always guaranteed present by that nudge.
	const prefixLen = len("2006-01-02T15:04:05.000")
	t = t.Truncate(time.Millisecond).Add(time.Millisecond / 10)
	b := t.AppendFormat(nil, time.RFC3339Nano)
	if len(b) <= prefixLen {
		return string(b), true
	}
	b = append(b[:prefixLen], b[prefixLen+1:]...)
	return string(b), true
}

// quoteSlogString renders s the way TextHandler renders a string: quoted
// with strconv.Quote when it needsSlogQuoting, verbatim otherwise.
func quoteSlogString(s string) string {
	if needsSlogQuoting(s) {
		return strconv.Quote(s)
	}
	return s
}

// needsSlogQuoting reproduces slog's TextHandler quoting rule exactly: a
// string needs quoting if it is empty, or contains a byte or rune that isn't
// safe to print unescaped in a key=value token — a Unicode space character, a
// non-printing character, '"', or '='.
func needsSlogQuoting(s string) bool {
	if len(s) == 0 {
		return true
	}
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			// A literal backslash is, unusually, left unquoted by
			// TextHandler even though it isn't in JSON's safe set — it only
			// needs escaping inside a JSON string, not a bare token.
			if b != '\\' && (b == ' ' || b == '=' || !isSafeSlogASCII(b)) {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return true
		}
		i += size
	}
	return false
}

// isSafeSlogASCII reports whether b is printable ASCII other than '"' and
// '\\', matching slog's unexported safeSet table.
func isSafeSlogASCII(b byte) bool {
	return b >= 0x20 && b < 0x7f && b != '"' && b != '\\'
}

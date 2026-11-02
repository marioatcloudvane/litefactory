package redact

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"
)

// ApplyReviewSource redacts complete decoded JSON string tokens without
// rebuilding an object or selecting between duplicate members. This also works
// on fenced or otherwise malformed submissions, provided every quoted value is
// complete and valid. Unsafe quoting or any truncation refuses the review.
func (r *Redactor) ApplyReviewSource(ctx context.Context, source string, limit int) (Body, error) {
	return r.applyReviewSource(ctx, source, limit, false)
}

// applyReviewSource in lenient mode serves displayed prose: a double quote that
// does not start a complete, valid JSON string is copied as ordinary text.
// Decodable values are still redacted, and once any quote was copied, a value
// starting at any remaining quote that would expose a secret refuses the text.
func (r *Redactor) applyReviewSource(ctx context.Context, source string, limit int, lenient bool) (Body, error) {
	if len(source) > limit || !utf8.ValidString(source) {
		return Body{}, fmt.Errorf("review source exceeds redaction bound or is invalid UTF-8")
	}
	var out strings.Builder
	stray := false
	for start := 0; start < len(source); {
		if err := ctx.Err(); err != nil {
			return Body{}, err
		}
		if source[start] != '"' {
			out.WriteByte(source[start])
			start++
			continue
		}
		end := start + 1
		for end < len(source) {
			if source[end] == '\\' {
				end += 2
				continue
			}
			if source[end] == '"' {
				break
			}
			end++
		}
		if end >= len(source) {
			if !lenient {
				return Body{}, fmt.Errorf("review contains an incomplete quoted value; safe retention unavailable")
			}
			// No later quote can close either: the escape alignment is shared.
			stray = true
			out.WriteString(source[start:])
			break
		}
		token := source[start : end+1]
		var value string
		if err := json.Unmarshal([]byte(token), &value); err != nil {
			if !lenient {
				return Body{}, fmt.Errorf("review contains an invalid quoted value; safe retention unavailable")
			}
			stray = true
			out.WriteByte('"')
			start++
			continue
		}
		safe, err := r.Apply(ctx, value, limit)
		if err != nil {
			return Body{}, err
		}
		if safe.Truncated() {
			return Body{}, fmt.Errorf("review string redaction exceeded budget")
		}
		if safe.String() == value {
			out.WriteString(token)
		} else {
			encoded, err := json.Marshal(safe.String())
			if err != nil {
				return Body{}, err
			}
			out.Write(encoded)
		}
		if out.Len() > limit {
			return Body{}, fmt.Errorf("review redaction exceeded budget")
		}
		start = end + 1
	}
	if stray {
		if err := r.refuseHiddenQuotedSecret(ctx, out.String(), limit); err != nil {
			return Body{}, err
		}
	}
	safe, err := r.Apply(ctx, out.String(), limit)
	if err != nil {
		return Body{}, err
	}
	if safe.Truncated() {
		return Body{}, fmt.Errorf("review source redaction exceeded budget")
	}
	return safe, nil
}

// refuseHiddenQuotedSecret checks every quote of leniently scanned text as a
// possible JSON string start. A copied stray quote shifts the pairing, so a
// value the scan did not decode could still hide an encoded secret.
func (r *Redactor) refuseHiddenQuotedSecret(ctx context.Context, text string, limit int) error {
	for start := strings.IndexByte(text, '"'); start >= 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + 1
		for end < len(text) {
			if text[end] == '\\' {
				end += 2
				continue
			}
			if text[end] == '"' {
				break
			}
			end++
		}
		if end >= len(text) {
			return nil
		}
		var value string
		if json.Unmarshal([]byte(text[start:end+1]), &value) == nil {
			safe, err := r.Apply(ctx, value, limit)
			if err != nil {
				return err
			}
			if safe.Truncated() || safe.String() != value {
				return fmt.Errorf("review contains an encoded secret in an ambiguous quoted value; safe retention unavailable")
			}
		}
		next := strings.IndexByte(text[start+1:], '"')
		if next < 0 {
			return nil
		}
		start += 1 + next
	}
	return nil
}

// ApplyReviewDisplay cleans factory-produced escaped pre/code spans before the
// final outbound pass. Decoding only these literal-text spans makes escaped
// credentials visible to the redactor without interpreting arbitrary Markdown
// or rebuilding an ambiguous submitted object. Unchanged spans keep their bytes.
func (r *Redactor) ApplyReviewDisplay(ctx context.Context, source string, limit int) (Body, error) {
	return r.applyReviewDisplay(ctx, source, limit, false)
}

// ApplyReviewJSONDisplay additionally cleans encoded JSON string values in
// complete JSON-looking pre spans. Use it before multipart splitting, then use
// ApplyReviewDisplay on each part, whose JSON tokens may be incomplete.
func (r *Redactor) ApplyReviewJSONDisplay(ctx context.Context, source string, limit int) (Body, error) {
	return r.applyReviewDisplay(ctx, source, limit, true)
}

func (r *Redactor) applyReviewDisplay(ctx context.Context, source string, limit int, completeJSON bool) (Body, error) {
	if len(source) > limit || !utf8.ValidString(source) {
		return Body{}, fmt.Errorf("review display exceeds redaction bound or is invalid UTF-8")
	}
	var out strings.Builder
	for len(source) > 0 {
		if err := ctx.Err(); err != nil {
			return Body{}, err
		}
		start, tag := nextLiteralOpening(source)
		if start < 0 {
			out.WriteString(source)
			break
		}
		opening, closing := "<"+tag+">", "</"+tag+">"
		// Factory submitted-source headings identify malformed review text too;
		// it must not evade decoded-string redaction by adding a prose prefix.
		factorySource := strings.HasSuffix(source[:start], "### Submitted content\n\n") || strings.HasSuffix(source[:start], "Complete submitted content (redacted, escaped; includes findings, uncertainty, questions and submitted coverage gaps):\n")
		out.WriteString(source[:start+len(opening)])
		source = source[start+len(opening):]
		end := strings.Index(source, closing)
		if end < 0 || strings.Contains(source[:end], "<") {
			// Not a factory span: agent text may mention the tag. Its tail, up to
			// the next opening tag, keeps the redaction a span would have had.
			tail := source
			if next, _ := nextLiteralOpening(source); next >= 0 {
				tail = source[:next]
			}
			decoded := html.UnescapeString(tail)
			if factorySource || tag == "pre" && reviewJSONText(decoded) {
				safe, err := r.applyReviewSource(ctx, decoded, limit, true)
				if err != nil {
					return Body{}, fmt.Errorf("review display has an unsafe literal-text span: %w", err)
				}
				if safe.Truncated() || safe.String() != decoded {
					return Body{}, fmt.Errorf("review display has an unsafe literal-text span")
				}
				out.WriteString(tail)
				source = source[len(tail):]
				continue
			}
			if decoded != tail {
				safe, err := r.Apply(ctx, decoded, limit)
				if err != nil {
					return Body{}, err
				}
				if safe.Truncated() || safe.String() != decoded {
					return Body{}, fmt.Errorf("review display has an unsafe literal-text span")
				}
			}
			continue
		}
		inner := source[:end]
		decoded := html.UnescapeString(inner)
		var safe Body
		var err error
		if tag == "pre" && completeJSON && (factorySource || reviewJSONText(decoded)) {
			safe, err = r.applyReviewSource(ctx, decoded, limit, true)
		} else {
			safe, err = r.Apply(ctx, decoded, limit)
		}
		if err != nil {
			return Body{}, err
		}
		if safe.Truncated() {
			return Body{}, fmt.Errorf("review display redaction exceeded budget")
		}
		if safe.String() == decoded {
			out.WriteString(inner)
		} else {
			out.WriteString(html.EscapeString(safe.String()))
		}
		out.WriteString(closing)
		if out.Len() > limit {
			return Body{}, fmt.Errorf("review display redaction exceeded budget")
		}
		source = source[end+len(closing):]
	}
	safe, err := r.Apply(ctx, out.String(), limit)
	if err != nil {
		return Body{}, err
	}
	if safe.Truncated() {
		return Body{}, fmt.Errorf("review display redaction exceeded budget")
	}
	return safe, nil
}

// nextLiteralOpening returns the index and name of the next opening pre or
// code tag, or -1.
func nextLiteralOpening(source string) (int, string) {
	pre, code := strings.Index(source, "<"+"pre>"), strings.Index(source, "<"+"code>")
	if pre < 0 || code >= 0 && code < pre {
		return code, "code"
	}
	return pre, "pre"
}

func reviewJSONText(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") || strings.HasPrefix(text, "\"") || strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~")
}

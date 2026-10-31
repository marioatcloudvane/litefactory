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
	if len(source) > limit || !utf8.ValidString(source) {
		return Body{}, fmt.Errorf("review source exceeds redaction bound or is invalid UTF-8")
	}
	var out strings.Builder
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
			return Body{}, fmt.Errorf("review contains an incomplete quoted value; safe retention unavailable")
		}
		token := source[start : end+1]
		var value string
		if err := json.Unmarshal([]byte(token), &value); err != nil {
			return Body{}, fmt.Errorf("review contains an invalid quoted value; safe retention unavailable")
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
	safe, err := r.Apply(ctx, out.String(), limit)
	if err != nil {
		return Body{}, err
	}
	if safe.Truncated() {
		return Body{}, fmt.Errorf("review source redaction exceeded budget")
	}
	return safe, nil
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
				safe, err := r.ApplyReviewSource(ctx, decoded, limit)
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
			safe, err = r.ApplyReviewSource(ctx, decoded, limit)
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

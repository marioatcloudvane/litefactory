package redact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"testing"
)

func TestApplyReviewSourcePreservesAmbiguityAndRedactsDecodedTokens(t *testing.T) {
	secret := "private<&\"credential-value-123"
	r := New(Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate and unknown fields, numeric spelling and wrapper bytes survive;
	// only sensitive string tokens may change, even in malformed JSON.
	source := "```json\n{\"unknown\":" + string(encoded) + ",\"unknown\":\"caveat\",\"n\":1.00}\n```"
	got, err := r.ApplyReviewSource(t.Context(), source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.String(), string(encoded)) || strings.Contains(got.String(), secret) {
		t.Fatalf("encoded credential survived: %q", got.String())
	}
	for _, part := range []string{"```json\n{\"unknown\":", ",\"unknown\":\"caveat\",\"n\":1.00}\n```"} {
		if !strings.Contains(got.String(), part) {
			t.Fatalf("source changed beyond redaction: %q", got.String())
		}
	}
	clean := "{\"unknown\":\"\\u0063aveat\",\"unknown\":\"second\"}"
	unchanged, err := r.ApplyReviewSource(t.Context(), clean, 8192)
	if err != nil || unchanged.String() != clean {
		t.Fatalf("unaffected tokens rewritten: %q %v", unchanged.String(), err)
	}
}

func TestApplyReviewDisplayCleansHTMLAndCompleteJSONBeforeSplitting(t *testing.T) {
	secret := "private<&\"credential-value-123"
	r := New(Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	source := "heading\n<pre>" + html.EscapeString("{\"unknown\":"+string(encoded)+",\"unknown\":\"caveat\"}") + "</pre>\n<code>" + html.EscapeString(secret) + "</code>"
	full, err := r.ApplyReviewJSONDisplay(t.Context(), source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	decoded := html.UnescapeString(full.String())
	if strings.Contains(decoded, string(encoded)) || strings.Contains(decoded, secret) || !strings.Contains(decoded, "caveat") {
		t.Fatalf("unsafe complete display: %q", full.String())
	}
	malformed := "### Submitted content\n\n<pre>" + html.EscapeString("preamble {\"value\":"+string(encoded)+"}") + "</pre>"
	full, err = r.ApplyReviewJSONDisplay(t.Context(), malformed, 8192)
	if err != nil || strings.Contains(html.UnescapeString(full.String()), string(encoded)) {
		t.Fatalf("malformed prefixed source escaped token redaction: %q %v", full.String(), err)
	}
	// Once a full source is clean, each part may contain a fragment of a JSON
	// string. The HTML-only pass must not demand complete string tokens again.
	part := "<pre>" + html.EscapeString("{\"value\":\"incomplete "+secret) + "</pre>"
	safe, err := r.ApplyReviewDisplay(t.Context(), part, 8192)
	if err != nil || strings.Contains(html.UnescapeString(safe.String()), secret) {
		t.Fatalf("part redaction: %q %v", safe.String(), err)
	}
	prose := "<pre>" + html.EscapeString("collector said \"unknown; "+secret) + "</pre>"
	safe, err = r.ApplyReviewJSONDisplay(t.Context(), prose, 8192)
	if err != nil || strings.Contains(html.UnescapeString(safe.String()), secret) {
		t.Fatalf("collector prose redaction: %q %v", safe.String(), err)
	}
	unchanged := "outside &amp; text <pre>{&#34;value&#34;:&#34;\\u0063aveat&#34;}</pre>"
	safe, err = r.ApplyReviewJSONDisplay(t.Context(), unchanged, 8192)
	if err != nil || safe.String() != unchanged {
		t.Fatalf("unaffected HTML rewritten: %q %v", safe.String(), err)
	}
}

func TestReviewRedactionFailsClosed(t *testing.T) {
	r := New(Options{Environ: []string{}})
	for _, source := range []string{`{"value":"unfinished`, `{"value":"\x20"}`, string([]byte{0xff})} {
		if _, err := r.ApplyReviewSource(t.Context(), source, 8192); err == nil {
			t.Fatalf("unsafe source accepted: %q", source)
		}
	}
	if _, err := r.ApplyReviewSource(t.Context(), strings.Repeat("x", 100), 32); err == nil {
		t.Fatal("oversize source accepted")
	}
	// An unclosed tag mention is ordinary text, but one that exposes an
	// HTML-escaped secret is still refused.
	secret := "private<&\"credential-value-123"
	withSecret := New(Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	if _, err := withSecret.ApplyReviewDisplay(t.Context(), preOpen+"unfinished "+html.EscapeString(secret), 8192); err == nil {
		t.Fatal("unfinished display exposing an escaped secret accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.ApplyReviewSource(ctx, "{}", 8192); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

var (
	preOpen   = "<" + "pre>"
	preClose  = "</" + "pre>"
	codeOpen  = "<" + "code>"
	codeClose = "</" + "code>"
)

// unicodeJSON encodes every character of s as a JSON Unicode escape.
func unicodeJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		fmt.Fprintf(&b, "\\u%04x", c)
	}
	b.WriteByte('"')
	return b.String()
}

func TestReviewDisplayTreatsTagMentionsAsText(t *testing.T) {
	r := New(Options{Environ: []string{}})
	for _, source := range []string{
		"The `" + preOpen + "` tag starts a span; prose mentions the " + codeOpen + " tag too.",
		"Ambiguous " + preOpen + "a " + codeOpen + "b" + codeClose + " c" + preClose + " end",
	} {
		for name, apply := range map[string]func(context.Context, string, int) (Body, error){"display": r.ApplyReviewDisplay, "json": r.ApplyReviewJSONDisplay} {
			got, err := apply(t.Context(), source, 8192)
			if err != nil || got.String() != source {
				t.Fatalf("%s: tag mention not kept as text: %q %v", name, got.String(), err)
			}
		}
	}
}

func TestReviewDisplayFakeSpansNeverLeakEncodedSecrets(t *testing.T) {
	secret := "private<&\"credential-value-123"
	r := New(Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	encoded := unicodeJSON(secret)
	value := html.EscapeString("{\"value\":" + encoded + "}")
	heading := "### Submitted content\n\n"
	ambiguous := codeOpen + "x" + codeClose + preClose
	for name, source := range map[string]string{
		"unclosed under heading":  heading + preOpen + value,
		"unclosed JSON-looking":   "intro " + preOpen + value,
		"ambiguous under heading": heading + preOpen + value + ambiguous,
		"ambiguous JSON-looking":  "intro " + preOpen + value + ambiguous,
	} {
		for mode, apply := range map[string]func(context.Context, string, int) (Body, error){"display": r.ApplyReviewDisplay, "json": r.ApplyReviewJSONDisplay} {
			got, err := apply(t.Context(), source, 8192)
			if err != nil {
				continue
			}
			out := got.String()
			decoded := html.UnescapeString(out)
			for _, text := range []string{out, decoded} {
				if strings.Contains(text, secret) || strings.Contains(text, encoded) || strings.Contains(text, html.EscapeString(secret)) {
					t.Fatalf("%s/%s: secret leaked: %q", name, mode, out)
				}
			}
			for i := strings.IndexByte(decoded, '"'); i >= 0; i = strings.IndexByte(decoded, '"') {
				decoded = decoded[i:]
				var s string
				if json.NewDecoder(strings.NewReader(decoded)).Decode(&s) == nil && strings.Contains(s, secret) {
					t.Fatalf("%s/%s: decoded JSON value leaked secret: %q", name, mode, out)
				}
				decoded = decoded[1:]
			}
		}
	}
}

func TestReviewDisplayTagMentionBeforeFactorySpanRedactsSpan(t *testing.T) {
	secret := "private<&\"credential-value-123"
	r := New(Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	source := "Mentions the `" + preOpen + "` tag.\n\n" + preOpen + html.EscapeString("value "+secret) + preClose
	got, err := r.ApplyReviewDisplay(t.Context(), source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.UnescapeString(got.String()), secret) || !strings.Contains(got.String(), "Mentions the `"+preOpen+"` tag.") || !strings.Contains(got.String(), Replacement) {
		t.Fatalf("span after tag mention not redacted: %q", got.String())
	}
}

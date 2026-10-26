package redact

import (
	"context"
	"encoding/json"
	"errors"
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
	if _, err := r.ApplyReviewDisplay(t.Context(), "<pre>unfinished", 8192); err == nil {
		t.Fatal("unfinished display accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.ApplyReviewSource(ctx, "{}", 8192); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

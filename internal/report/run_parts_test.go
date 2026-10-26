package report_test

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/redact"
)

func TestCompleteReviewPartsPreserveDisplayAndRedactEncodedSecrets(t *testing.T) {
	t.Parallel()
	const secret = "review-secret-<&>-123456"
	reporter := newReporter(t, func(o *redact.Options) { o.Environ = []string{"API_TOKEN=" + secret} })
	data, _ := json.Marshal(map[string]string{"unknown": strings.Repeat("λ&<> evidence remains uncertain. ", 8000), "credential": secret, "path": "/Users/private/.litefactory/runs/review.txt"})
	original := "## Retained result — unaccepted, for review only\n\n<pre>" + html.EscapeString(string(data)) + "</pre>"
	run := successRun()
	run.Result.ResultText = original
	summary, parts, err := reporter.RenderRunParts(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 3 || !strings.Contains(summary.String(), "numbered parts") {
		t.Fatal("missing complete report parts")
	}
	var joined strings.Builder
	for i, part := range parts {
		text := part.String()
		if !utf8.ValidString(text) || part.Truncated() {
			t.Fatal("invalid or truncated part")
		}
		if strings.Contains(text, "/Users/private/") {
			t.Fatal("escaped host path exposed")
		}
		if strings.Contains(text, secret) || strings.Contains(html.UnescapeString(text), secret) {
			t.Fatal("encoded credential exposed")
		}
		if strings.Count(text, "<pre>") != strings.Count(text, "</pre>") {
			t.Fatalf("part %d has unbalanced review context", i)
		}
		prefix := fmt.Sprintf("Full retained result — part %d/%d\n\n", i+1, len(parts))
		if !strings.HasPrefix(text, prefix) {
			t.Fatal("unordered part")
		}
		chunk := strings.TrimPrefix(text, prefix)
		chunk = strings.TrimPrefix(chunk, "<!-- lf:part-context-open --><pre>")
		chunk = strings.TrimSuffix(chunk, "</pre><!-- lf:part-context-close -->")
		joined.WriteString(chunk)
	}
	expected := strings.ReplaceAll(original, html.EscapeString(`review-secret-\u003c\u0026\u003e-123456`), "[redacted]")
	expected = strings.ReplaceAll(expected, "/Users/private/.litefactory/runs/review.txt", "…/runs/review.txt")
	// JSON encoding uses HTML-safe escapes. The source remains otherwise exact.
	if joined.String() != expected {
		t.Fatal("complete review changed content beyond credential redaction")
	}
	if run.Result.ResultText != original {
		t.Fatal("rendering changed source")
	}
}

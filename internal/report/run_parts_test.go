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
	"github.com/cloudvane/litefactory/internal/report"
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

// A retained unaccepted result whose prose holds a stray double quote is still
// published, and its encoded secret token is still redacted.
func TestRetainedResultWithStrayQuoteIsPublishedAndRedacted(t *testing.T) {
	t.Parallel()
	const secret = "review-secret-<&>-123456"
	dq, bs := string(rune(34)), string(rune(92))
	reporter := newReporter(t, func(o *redact.Options) { o.Environ = []string{"API_TOKEN=" + secret} })
	encoded, _ := json.Marshal(secret)
	prose := "collector said " + dq + " and stopped"
	cell := dq + "a " + bs + "| b" + dq
	content := "{" + dq + "credential" + dq + ":" + string(encoded) + "," + dq + "note" + dq + ":" + cell + ", " + prose + "}"
	text := "## Retained result — unaccepted, for review only\n\n<pre>" + html.EscapeString(content) + "</pre>"
	summary, parts, err := reporter.RenderTextParts(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	out := summary.String()
	for _, part := range parts {
		out += part.String()
	}
	decoded := html.UnescapeString(out)
	if !strings.Contains(decoded, prose) || !strings.Contains(decoded, cell) {
		t.Fatalf("prose lost: %q", out)
	}
	for _, view := range []string{out, decoded} {
		if strings.Contains(view, secret) || strings.Contains(view, string(encoded)) {
			t.Fatalf("secret exposed: %q", out)
		}
	}
}

// A review that is one fenced block stays code in every part, and removing
// the labelled context rebuilds it exactly, even when the size would cut
// into the closing fence.
func TestSplitReviewPartsKeepsFencedBlockReadable(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("it's \"λ\" & <kept> ``` x\n", 40)
	fence := report.CodeFence(content)
	text := fence + "\n" + content + fence
	for _, size := range []int{128, 300, len(text) - 2, len(text) - len(fence)} {
		chunks, err := report.SplitReviewParts(text, size)
		if err != nil || len(chunks) < 2 {
			t.Fatalf("size %d: %d parts, err=%v", size, len(chunks), err)
		}
		var joined strings.Builder
		for i, chunk := range chunks {
			if !utf8.ValidString(chunk) || strings.Contains(chunk, "<pre>") {
				t.Fatalf("size %d part %d is not plain code", size, i)
			}
			fences := 0
			for _, line := range strings.Split(chunk, "\n") {
				if line == fence {
					fences++
				}
			}
			if fences != 2 {
				t.Fatalf("size %d part %d has %d fence lines", size, i, fences)
			}
			chunk = strings.TrimPrefix(chunk, "<!-- lf:part-context-open -->\n"+fence+"\n")
			chunk = strings.TrimSuffix(chunk, "\n"+fence+"\n<!-- lf:part-context-close -->")
			joined.WriteString(chunk)
		}
		if joined.String() != text {
			t.Fatalf("size %d: parts do not rebuild the block", size)
		}
	}
}

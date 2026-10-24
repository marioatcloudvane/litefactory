package report

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/redact"
)

// RenderRunParts returns a short report and the complete result, when the
// ordinary report would shorten it. Every part is redacted before retention.
// Input limits belong to the submission adapter; this renderer never shrinks
// input to make it pass a submission limit.
func (r *Reporter) RenderRunParts(ctx context.Context, run Run) (redact.Body, []redact.Body, error) {
	if run.Result == nil || strings.TrimSpace(run.Result.ResultText) == "" {
		body, err := r.RenderRun(ctx, run)
		return body, nil, err
	}
	// Clean the complete review before HTML escaping or JSON string encoding can
	// hide a literal credential. Parts are rechecked after context is added.
	if strings.Contains(run.Result.ResultText, "## Retained result — unaccepted, for review only") {
		safe, err := r.redactor.ApplyReviewJSONDisplay(ctx, run.Result.ResultText, 64<<20)
		if err != nil {
			return redact.Body{}, nil, err
		}
		if safe.Truncated() {
			return redact.Body{}, nil, fmt.Errorf("complete review redaction exceeded budget")
		}
		run.Result = run.Result.Clone()
		run.Result.ResultText = safe.String()
	}
	_, cuts := r.runBody(run)
	text := run.Result.ResultText
	if len(text) <= maxResultBytes-len(resultOverlongNotice)-maxFenceBytes && !cuts.result {
		body, err := r.RenderRun(ctx, run)
		return body, nil, err
	}
	if len(text) > 64<<20 || !utf8.ValidString(text) {
		return redact.Body{}, nil, fmt.Errorf("complete report result exceeds presentation bound or is invalid UTF-8")
	}
	// Redact the complete source before splitting, so credentials spanning a
	// part boundary cannot escape the hygiene pass.
	safe, err := r.redactor.ApplyReviewDisplay(ctx, StripHostPaths(text, r.hostRoots), 64<<20)
	if err != nil {
		return redact.Body{}, nil, err
	}
	if safe.Truncated() {
		return redact.Body{}, nil, fmt.Errorf("complete report redaction exceeded budget")
	}
	text = safe.String()
	size := min(48000, r.budget-2048)
	if size < 512 {
		return redact.Body{}, nil, fmt.Errorf("comment limit cannot hold complete report parts")
	}
	chunks, err := SplitReviewParts(text, size)
	if err != nil {
		return redact.Body{}, nil, err
	}
	parts := make([]redact.Body, 0, len(chunks))
	for i, chunk := range chunks {
		body, e := r.RenderReportDelivery(ctx, fmt.Sprintf("Full retained result — part %d/%d\n\n%s", i+1, len(chunks), chunk), "")
		if e != nil {
			return redact.Body{}, nil, e
		}
		parts = append(parts, body)
	}
	run.Result = run.Result.Clone()
	run.Result.ResultText = "Complete retained content is delivered in the numbered parts linked below. Review content does not change workflow acceptance or approval."
	summary, err := r.RenderRun(ctx, run)
	return summary, parts, err
}

// Keep escaped review markup readable when a comment boundary falls inside a
// preformatted block or an HTML entity. Added context is labelled separately
// from source bytes and is not part of the retained content.
func safeReviewBoundary(text string, n int) int {
	if n == len(text) {
		return n
	}
	for _, delimiters := range [][2]string{{"&", ";"}, {"<", ">"}} {
		start := strings.LastIndex(text[:n], delimiters[0])
		if start >= 0 && n-start < 32 && !strings.Contains(text[start:n], delimiters[1]) {
			if end := strings.Index(text[n:], delimiters[1]); end >= 0 && end < 32 {
				n = start
			}
		}
	}
	return n
}
func reviewPreContext(text string, active bool) bool {
	for {
		opening := strings.Index(text, "<pre>")
		closing := strings.Index(text, "</pre>")
		switch {
		case opening < 0 && closing < 0:
			return active
		case opening >= 0 && (closing < 0 || opening < closing):
			active = true
			text = text[opening+len("<pre>"):]
		default:
			active = false
			text = text[closing+len("</pre>"):]
		}
	}
}

// SplitReviewParts keeps literal review spans readable in each independently
// rendered comment. Context wrappers carry explicit markers so concatenating
// the source portions reconstructs the complete original text.
func SplitReviewParts(text string, size int) ([]string, error) {
	if size < 128 || !utf8.ValidString(text) {
		return nil, fmt.Errorf("invalid review part size or UTF-8")
	}
	var chunks []string
	var contexts []bool
	inPre := false
	for len(text) > 0 {
		n := min(size, len(text))
		for n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		n = safeReviewBoundary(text, n)
		contexts = append(contexts, inPre)
		inPre = reviewPreContext(text[:n], inPre)
		chunks = append(chunks, text[:n])
		text = text[n:]
	}
	for i, chunk := range chunks {
		if contexts[i] {
			chunk = "<!-- lf:part-context-open --><pre>" + chunk
		}
		if i+1 < len(chunks) && contexts[i+1] {
			chunk += "</pre><!-- lf:part-context-close -->"
		}
		chunks[i] = chunk
	}
	return chunks, nil
}

// RenderTextParts serves explicit workflow comment activities, which carry no
// ordinary run-report metadata. It retains complete owner output and does not
// interpret rejected content as an accepted workflow result.
func (r *Reporter) RenderTextParts(ctx context.Context, text string) (redact.Body, []redact.Body, error) {
	var safe redact.Body
	var err error
	text = StripHostPaths(text, r.hostRoots)
	if strings.Contains(text, "## Retained result — unaccepted, for review only") {
		safe, err = r.redactor.ApplyReviewJSONDisplay(ctx, text, 64<<20)
	} else {
		safe, err = r.redactor.ApplyReviewDisplay(ctx, text, 64<<20)
	}
	if err != nil {
		return redact.Body{}, nil, err
	}
	if safe.Truncated() {
		return redact.Body{}, nil, fmt.Errorf("complete workflow output exceeds redaction allowance")
	}
	text = safe.String()
	if len(text) <= r.budget-1024 {
		body, e := r.RenderReportDelivery(ctx, text, "")
		return body, nil, e
	}
	size := min(48000, r.budget-2048)
	if size < 512 {
		return redact.Body{}, nil, fmt.Errorf("comment limit cannot hold complete workflow output")
	}
	chunks, err := SplitReviewParts(text, size)
	if err != nil {
		return redact.Body{}, nil, err
	}
	parts := make([]redact.Body, 0, len(chunks))
	for i, chunk := range chunks {
		body, e := r.RenderReportDelivery(ctx, fmt.Sprintf("Full retained result — part %d/%d\n\n%s", i+1, len(chunks), chunk), "")
		if e != nil {
			return redact.Body{}, nil, e
		}
		parts = append(parts, body)
	}
	summary, err := r.RenderReportDelivery(ctx, "Complete workflow output is available in the numbered parts linked below. Retained unaccepted review content does not grant approval, satisfy workflow validation or enable actions.", "")
	return summary, parts, err
}

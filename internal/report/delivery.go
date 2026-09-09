package report

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/redact"
)

var deliveryClosingDirective = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:es|ed)?|resolve[sd]?)\s*:?\s+(?:#[0-9]|[a-z0-9_.-]+/[a-z0-9_.-]+#[0-9]|https?://)`)

// DeliveryContentSafe rejects issue-closing directives before creation or merge.
// Owner reasons and remotely edited PR text cannot bypass ordered closure.
func DeliveryContentSafe(title, body string) bool {
	return !deliveryClosingDirective.MatchString(title + "\n" + body)
}

// RenderDeliveryContent produces complete reviewable PR text, reserving room for
// the concrete approval command in its parent notice.
func (r *Reporter) RenderDeliveryContent(ctx context.Context, title, body string) (redact.Body, redact.Body, error) {
	if !DeliveryContentSafe(title, body) {
		return redact.Body{}, redact.Body{}, fmt.Errorf("delivery content contains automatic issue-closing directive")
	}
	if r.limit <= 2048 {
		return redact.Body{}, redact.Body{}, fmt.Errorf("delivery report limit too small")
	}
	t, err := r.redactor.Apply(ctx, title, 240)
	if err != nil {
		return t, redact.Body{}, err
	}
	b, err := r.redactor.Apply(ctx, body, r.limit-2048)
	if err != nil {
		return t, b, err
	}
	if t.Truncated() || b.Truncated() {
		return t, b, fmt.Errorf("complete delivery proposal exceeds report limit")
	}
	return t, b, nil
}

// RenderDeliveryNotice rejects truncated approval metadata or success evidence.
func (r *Reporter) RenderDeliveryNotice(ctx context.Context, detail, marker string) (redact.Body, error) {
	b, err := r.redactor.Apply(ctx, detail+"\n\n"+marker, r.limit)
	if err != nil {
		return b, err
	}
	if b.Truncated() || !strings.HasSuffix(b.String(), marker) {
		return b, fmt.Errorf("complete delivery notice exceeds report limit")
	}
	return b, nil
}

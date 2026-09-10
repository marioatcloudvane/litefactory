package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/redact"
)

// RenderFollowupContent refuses truncation so approval always covers full content.
func (r *Reporter) RenderFollowupContent(ctx context.Context, title, body string) (redact.Body, redact.Body, error) {
	t, err := r.redactor.Apply(ctx, title, 240)
	if err != nil {
		return t, redact.Body{}, err
	}
	// Reserve full title, identity, approval instructions and report marker so
	// every retained proposal can actually be displayed without truncation.
	const proposalEnvelopeReserve = 2048
	if r.limit <= proposalEnvelopeReserve {
		return t, redact.Body{}, fmt.Errorf("report limit cannot hold complete followup proposal")
	}
	b, err := r.redactor.Apply(ctx, body, r.limit-proposalEnvelopeReserve)
	if err != nil {
		return t, b, err
	}
	if t.Truncated() || b.Truncated() {
		return t, b, fmt.Errorf("complete followup content exceeds report limit")
	}
	return t, b, nil
}

// RenderFollowupNotice renders complete proposal or outcome details with a stable marker.
func (r *Reporter) RenderFollowupNotice(ctx context.Context, detail, marker string) (redact.Body, error) {
	b, err := r.redactor.Apply(ctx, detail+"\n\n"+marker, r.limit)
	if err != nil {
		return b, err
	}
	if b.Truncated() || !strings.HasSuffix(b.String(), marker) {
		return b, fmt.Errorf("complete followup notice exceeds report limit")
	}
	return b, nil
}

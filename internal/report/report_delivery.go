package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/redact"
)

// RenderReportDelivery applies the configured outbound hygiene to an already
// rendered report and its durable marker. An empty marker restores retained
// content; callers require byte equality before dispatching a saved intent.
func (r *Reporter) RenderReportDelivery(ctx context.Context, body, marker string) (redact.Body, error) {
	if marker != "" {
		body += "\n\n" + marker
	}
	value, err := r.redactor.Apply(ctx, body, r.limit)
	if err != nil {
		return value, err
	}
	if value.Truncated() || (marker != "" && !strings.HasSuffix(value.String(), marker)) {
		return value, fmt.Errorf("report delivery exceeds the complete-content limit")
	}
	return value, nil
}

package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// AcceptanceNotice describes attributed acceptance findings and ordered closure intent.
type AcceptanceNotice struct {
	Parent                            naming.Ref
	ID, RunID                         int64
	OwnerExceptionID                  int64
	Generation                        int
	Story, Revision, Snapshot, Detail string
	Accepted                          bool
}

// Marker returns the stable acceptance-report reconciliation marker.
func (n AcceptanceNotice) Marker() string {
	return fmt.Sprintf("<!-- litefactory:acceptance:%s:%d -->", n.Parent.String(), n.ID)
}

// RenderAcceptance renders factual and owner-waived findings without conflating them.
func (r *Reporter) RenderAcceptance(ctx context.Context, n AcceptanceNotice) (redact.Body, error) {
	if err := n.Parent.Validate(); err != nil {
		return redact.Body{}, err
	}
	if n.ID < 1 || (n.RunID < 1 && n.OwnerExceptionID < 1) || n.Generation < 1 || !scopeKeyPattern.MatchString(n.Story) || !scopeDigestPattern.MatchString(n.Revision) || (n.Snapshot != "" && !scopeDigestPattern.MatchString(n.Snapshot)) {
		return redact.Body{}, fmt.Errorf("invalid acceptance report")
	}
	state := "Unmet or unverified obligations remain; no acceptance is claimed."
	if n.Accepted {
		state = "Acceptance obligations satisfied or explicitly owner-waived. Closure follows successful checkbox projection."
	}
	if n.RunID == 0 {
		n.Detail = fmt.Sprintf("Owner exception %d; no validation assessment exists. Original obligations remain factually unverified.\n%s", n.OwnerExceptionID, n.Detail)
	}
	detail := strings.ReplaceAll(strings.ReplaceAll(n.Detail, "\r", ""), "\n", "\n    ")
	text := fmt.Sprintf("Story %s — %s\n\nGeneration %d; validation run %d; frozen revision `%s`; retained snapshot `%s`.\n\n    %s\n\n%s", n.Story, state, n.Generation, n.RunID, n.Revision, n.Snapshot, detail, n.Marker())
	body, err := r.redactor.Apply(ctx, text, r.limit)
	if err != nil {
		return redact.Body{}, err
	}
	if body.Truncated() || !strings.HasSuffix(body.String(), n.Marker()) {
		return redact.Body{}, fmt.Errorf("acceptance report exceeds limit")
	}
	return body, nil
}

// RenderAcceptanceProjection refuses any redaction that would change the live
// requirements. Hygiene remains mandatory, but never silently rewrites scope.
func (r *Reporter) RenderAcceptanceProjection(ctx context.Context, title, body string) (redact.Body, redact.Body, error) {
	t, err := r.redactor.Apply(ctx, title, 256)
	if err != nil {
		return redact.Body{}, redact.Body{}, err
	}
	b, err := r.redactor.Apply(ctx, body, 65536)
	if err != nil {
		return redact.Body{}, redact.Body{}, err
	}
	if t.Truncated() || b.Truncated() || t.String() != title || b.String() != body {
		return redact.Body{}, redact.Body{}, fmt.Errorf("acceptance projection would change or truncate requirements during redaction")
	}
	return t, b, nil
}

package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// OwnerExceptionNotice describes a factory-admitted exception or control refusal.
type OwnerExceptionNotice struct {
	Parent             string
	CommentID, ActorID int64
	Detail             string
	Accepted           bool
	// EditRefused marks the single reply to an edited source comment whose
	// original exception is already retained. It uses its own stable marker so
	// that it never collides with the retained exception's acknowledgement.
	EditRefused bool
}

// Marker returns the stable source-comment acknowledgement marker.
func (n OwnerExceptionNotice) Marker() string {
	if n.EditRefused {
		return fmt.Sprintf("<!-- litefactory:owner-exception:%s:%d:edit-refused -->", n.Parent, n.CommentID)
	}
	return fmt.Sprintf("<!-- litefactory:owner-exception:%s:%d -->", n.Parent, n.CommentID)
}

// RenderOwnerException renders bounded exception authority and preserved factual limits.
func (r *Reporter) RenderOwnerException(ctx context.Context, n OwnerExceptionNotice) (redact.Body, error) {
	if _, err := naming.ParseRef(n.Parent); err != nil {
		return redact.Body{}, err
	}
	if n.CommentID < 1 || n.ActorID < 1 {
		return redact.Body{}, fmt.Errorf("invalid exception notice")
	}
	state := "Exception refused; no acceptance authority recorded."
	switch {
	case n.EditRefused:
		state = "Edited exception comment ignored; the originally retained owner exception stays authoritative and unchanged."
	case n.Accepted:
		state = "Owner exception recorded. Original failed/unverified evidence remains unchanged. This is not merge approval."
	}
	detail := strings.ReplaceAll(strings.ReplaceAll(n.Detail, "\r", ""), "\n", "\n    ")
	body, err := r.redactor.Apply(ctx, fmt.Sprintf("%s\n\nOwner ID %d; source comment %d.\n\n    %s\n\n%s", state, n.ActorID, n.CommentID, detail, n.Marker()), r.limit)
	if err != nil {
		return redact.Body{}, err
	}
	if body.Truncated() || !strings.HasSuffix(body.String(), n.Marker()) {
		return redact.Body{}, fmt.Errorf("exception report exceeds limit")
	}
	return body, nil
}

package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// CreationRejectionError classifies only confirmed input refusal at its origin.
type CreationRejectionError struct {
	Reason string
	Cause  error
}

func (e *CreationRejectionError) Error() string { return "creation command rejected: " + e.Reason }
func (e *CreationRejectionError) Unwrap() error { return e.Cause }
func rejectCreation(reason string, cause error) error {
	return &CreationRejectionError{Reason: reason, Cause: cause}
}

// CreationRejectionReason leaves unknown and operational failures retryable.
func CreationRejectionReason(err error) (string, bool) {
	// A joined rollback/storage failure makes the observation uncertain even
	// when another branch contains a verified policy refusal.
	for err != nil {
		switch value := err.(type) { //nolint:errorlint // Walk one wrapper at a time: errors.As could cross a joined operational failure and misclassify it as rejection.
		case interface{ Unwrap() []error }:
			return "", false
		case *CreationRejectionError:
			return value.Reason, true
		case *store.CreationPolicyRefusal:
			return "proposal", true
		}
		err = errors.Unwrap(err)
	}
	return "", false
}

// ReportCreationRejection retains safe output and its identity before creating
// report debt. Recovery fills the outcome-to-delivery crash gap before parsing.
func (r *Reporter) ReportCreationRejection(ctx context.Context, ref naming.Ref, sessionID, commentID int64, kind, reason string) (Posted, error) {
	if err := ref.Validate(); err != nil {
		return Posted{}, err
	}
	old, err := r.db.GetCreationRejection(ctx, sessionID, commentID)
	if err == nil {
		return r.resumeCreationRejection(ctx, old)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	intent := r.deliveryIntent(ref, ref, sessionID, commentID, 0, "creation-rejection")
	body, err := r.renderer.RenderReply(ctx, report.Reply{Ref: ref, Kind: report.ReplyCreationRejected, CreationRejection: &report.CreationRejection{CommandKind: kind, Reason: reason}})
	if err != nil {
		return Posted{}, err
	}
	if body.Truncated() {
		return Posted{}, errors.New("creation rejection guidance truncated")
	}
	body, err = r.renderer.RenderReportDelivery(ctx, body.String(), intent.Marker)
	if err != nil {
		return Posted{}, err
	}
	if body.Truncated() {
		return Posted{}, errors.New("creation rejection delivery truncated")
	}
	intent.Body = body.String()
	saved, err := r.db.RetainCreationRejection(ctx, store.CreationRejection{Repository: ref.Owner + "/" + ref.Repo, SourceTask: ref.String(), SessionID: sessionID, CommentID: commentID, CommandKind: kind, Reason: reason, Delivery: intent})
	if err != nil {
		return Posted{}, err
	}
	return r.resumeCreationRejection(ctx, saved)
}
func (r *Reporter) resumeCreationRejection(ctx context.Context, row store.CreationRejection) (Posted, error) {
	saved, err := r.db.EnsureReportDelivery(ctx, row.Delivery)
	if err != nil {
		return Posted{}, err
	}
	return r.dispatchDelivery(ctx, saved)
}

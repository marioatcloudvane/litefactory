package attach

// RejectionReason is why litefactory declined to ingest an attachment.
//
// The zero value, ReasonInvalid, names nothing; it exists so a
// zero-initialized [Rejection] fails [NewRejection]'s validation rather than
// silently claiming a reason it does not have.
type RejectionReason int

const (
	// ReasonInvalid is the zero value. It is never a reason a caller should
	// construct on purpose.
	ReasonInvalid RejectionReason = iota

	// ReasonOversized means the attachment exceeded the per-file size
	// ceiling for its type (attachment_max_image_bytes or
	// attachment_max_document_bytes) — AC-6.1.
	ReasonOversized

	// ReasonUnsupportedType means the attachment's sniffed content type is
	// not one litefactory accepts. This is always decided from what the
	// bytes actually are, never from a declared filename extension or a
	// forge-supplied content-type header — AC-6.2.
	ReasonUnsupportedType

	// ReasonFetchFailed means litefactory could not retrieve the attachment
	// (a network error, or a forge response other than 401/404, which are
	// handled elsewhere as access/absence rather than a fetch failure) —
	// AC-6.4.
	ReasonFetchFailed

	// ReasonOverCount means the turn had already reached
	// attachment_max_count before this attachment was considered. Turn-wide
	// ceilings are evaluated before any per-file ceiling, so an attachment
	// can carry this reason even though it would have fit within its own
	// type's size limit — AC-6.3.
	ReasonOverCount

	// ReasonOverTotalBytes means accepting this attachment would have
	// pushed the turn's combined attachment size past
	// attachment_max_total_bytes. Like ReasonOverCount, this is evaluated
	// before any per-file ceiling — AC-6.3.
	ReasonOverTotalBytes
)

// String returns a short, stable, snake_case token for r, suitable for logs
// and for a manifest record's machine-readable reason column. It is not the
// text shown to a person; see [RejectionReason.ReasonText] for that.
func (r RejectionReason) String() string {
	switch r {
	case ReasonOversized:
		return "oversized"
	case ReasonUnsupportedType:
		return "unsupported_type"
	case ReasonFetchFailed:
		return "fetch_failed"
	case ReasonOverCount:
		return "over_count"
	case ReasonOverTotalBytes:
		return "over_total_bytes"
	default:
		return "invalid"
	}
}

// valid reports whether r is one of the five real reasons rather than the
// zero value or an out-of-range int.
func (r RejectionReason) valid() bool {
	switch r {
	case ReasonOversized, ReasonUnsupportedType, ReasonFetchFailed, ReasonOverCount, ReasonOverTotalBytes:
		return true
	default:
		return false
	}
}

// ReasonText returns the reason-only sentence fragment for r: the second
// half of a line like "`report.pdf` was too large." It is litefactory's own
// classification, never text that reached the process from a third party,
// so unlike an attachment's name (see [Rejection.Line]) it needs no
// sanitization before it is stated directly in a comment.
func (r RejectionReason) ReasonText() string {
	switch r {
	case ReasonOversized:
		return "was too large"
	case ReasonUnsupportedType:
		return "is an unsupported file type"
	case ReasonFetchFailed:
		return "could not be fetched"
	case ReasonOverCount:
		return "was dropped because this turn already reached its maximum number of attachments"
	case ReasonOverTotalBytes:
		return "was dropped because this turn already reached its maximum total attachment size"
	default:
		return "was rejected"
	}
}

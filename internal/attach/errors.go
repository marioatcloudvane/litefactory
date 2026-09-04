package attach

import "errors"

// ErrInvalidRejection is the sentinel for a caller mistake building a
// [Rejection]: an empty name or an invalid [RejectionReason]. It never
// represents an attachment that failed to qualify — that outcome is the
// [Rejection] value itself, not an error (see the package doc comment,
// "Rejections are values, not errors").
var ErrInvalidRejection = errors.New("attach: invalid rejection")

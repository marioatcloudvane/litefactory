package naming

import "errors"

// The sentinel errors this package returns. Every error value returned by
// naming wraps exactly one of these, so callers can branch with errors.Is
// without matching on message text.
//
// These are deliberately not [cli.PreconditionError] or any other transport
// type: domain packages do not import the command layer, which translates
// them.
var (
	// ErrInvalidRef reports a task ref that is not in canonical
	// forge:owner/name#issue form, or whose components are unusable as path
	// segments.
	ErrInvalidRef = errors.New("invalid task ref")

	// ErrInvalidSlug reports a feature slug that fails the post-condition
	// pattern in [ValidateSlug]. Seeing this from [Slug] means the derivation
	// itself is broken; seeing it from [Branch] means a stored slug was
	// corrupted after it was frozen.
	ErrInvalidSlug = errors.New("invalid feature slug")

	// ErrInvalidFeaturePrefix reports a configured feature_prefix that git
	// would reject as the leading component of a branch name.
	ErrInvalidFeaturePrefix = errors.New("invalid feature prefix")

	// ErrInvalidPath reports a path root that cannot be used to build a
	// worktree or log path.
	ErrInvalidPath = errors.New("invalid path")

	// ErrInvalidOrdinal reports a run or segment ordinal below its first
	// legal value. Both count from 1.
	ErrInvalidOrdinal = errors.New("invalid ordinal")

	// ErrInvalidSessionID reports a derived Claude session id that is not a
	// canonical UUID. Like ErrInvalidSlug this is an assertion failure, not a
	// user error.
	ErrInvalidSessionID = errors.New("invalid claude session id")
)

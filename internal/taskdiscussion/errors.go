package taskdiscussion

import "errors"

var (
	// ErrTargetUnavailable means no single permitted current saved target exists.
	ErrTargetUnavailable = errors.New("current advisory target is unavailable or ambiguous")
	// ErrBasisChanged means evidence changed while an admission was being captured.
	ErrBasisChanged = errors.New("task basis changed during advisory admission")
)

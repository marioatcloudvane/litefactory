package report

import "errors"

// ErrInvalidArgument is the sentinel for a caller mistake: a malformed task
// ref, a run ordinal that is not a run number, a reply kind that is not one of
// the three.
//
// It is deliberately not used for missing run data. A run that finished with
// half its fields unknown still gets a comment — AC-4.1 says silence is never
// an outcome — so a sparse [Run] renders "unknown" where it has to and never
// errors. Only inputs that make the marker itself unrenderable fail here,
// because a body without a well-formed marker breaks the self-filter and the
// daemon would then read its own comment as an instruction.
var ErrInvalidArgument = errors.New("report: invalid argument")

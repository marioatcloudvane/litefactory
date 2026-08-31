// Package planbrief reads plan_brief.md's Story Index and the per-story
// sub-issue bodies a Plan run writes beside it.
//
// # Pure text in, structs out
//
// Nothing here does I/O, talks to git, or reaches the forge. Every function
// takes bytes it was handed and returns either a struct or rewritten bytes —
// internal/turn owns reading the files off disk, calling the forge and
// writing the result back, and this package exists so that the Story Index
// table format (owned by plan-orchestrator.md's contract) does not leak into
// that orchestration logic. See internal/naming for the sibling package that
// holds itself to the same discipline, for the same reason.
//
// # What it is not
//
// It does not decide which stories still need a sub-issue — that answer
// comes from GitHub, not from this package or from the file (see
// internal/turn's sub-issue phase for why). It only parses what is on the
// page and rewrites the one cell it is told to.
package planbrief

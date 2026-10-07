// Package planbrief validates complete Plan proposals.
//
// # Pure text in, structs out
//
// Nothing here does I/O, talks to git, or reaches the forge. Every function
// takes bytes it was handed and returns either a struct or rewritten bytes —
// internal/turn owns reading the files off disk, calling the forge and
// writing the result back, and this package exists so that Plan artefact
// formats do not leak into that orchestration logic. See internal/naming for
// the sibling package that holds itself to the same discipline, for the same
// reason.
//
// # What it is not
//
// It does not decide which stories still need a sub-issue — that answer
// comes from the managed publication owner in internal/turn. Nothing here
// discovers or publishes per-story files.
package planbrief

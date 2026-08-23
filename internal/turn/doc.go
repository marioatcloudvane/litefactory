// Package turn executes one comment.
//
// A turn is the whole business path triggered by a single instruction comment:
// parse it, take the per-task lock, prepare the worktree, run the harness,
// harvest the result. It deliberately knows nothing about time — no ticker, no
// signals, no backoff. That belongs to internal/daemon, which is about when
// turns happen; this package is about what one turn does, so the entire path
// is testable without a clock.
//
// [ParseCommand] is the front door: it decides whether a comment body asks for
// anything at all, and is a pure function with no dependencies.
package turn

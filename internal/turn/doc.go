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
// anything at all, and is a pure function with no dependencies. [Execute] is
// everything after it.
//
// # A bad turn is not an error
//
// [Execute] returns a [Result] for every way a turn can go wrong that the
// person who wrote the comment needs to hear about: a fetch that failed, a
// worktree path someone is standing in, a claude that exited non-zero, timed
// out or produced nothing. Silence is the one outcome that is not allowed
// (AC-4.1), and a value is what carries a report out of here. The error return
// is reserved for a turn that could not be conducted or recorded at all —
// invalid dependencies, an invalid request, a database that will not answer.
//
// # The lock is the concurrency model
//
// One task, one lock, no queue. The lock is taken before any preparation and
// released on every exit path including a panic (AC-3.1, AC-3.3). A turn that
// finds the lock held does nothing and says so, and the comment stays
// unconsumed so a later tick sees it again. Everything else — two daemons, a
// crash mid-run, the same comment arriving twice — is handled by that plus the
// uniqueness of (session, triggering comment) on the runs row, which makes a
// replayed comment a no-op rather than a second run (AC-3.4).
//
// # Post first, then commit
//
// [Reporter] is the other half of a turn: it posts the comment and only then
// records that it did. The result comment id, the watermark advance and the
// run's move to reported are one transaction that runs after the post returns,
// so a forge that refuses the comment leaves the run un-reported and the
// comment unconsumed — and the next tick retries the post rather than claude
// (AC-4.10). Advancing first would turn a transient 502 into permanent silence.
//
// # The registered clone is never written to
//
// A turn fetches in the clone and works in a worktree. Nothing here can write
// inside the repository the user registered, because internal/git does not
// expose a way to: the clone's method set is the S-6 allowlist and every
// mutating operation belongs to a worktree. The guarantee is asserted end to
// end, across a full turn, against a deliberately dirtied fixture (AC-3.10).
package turn

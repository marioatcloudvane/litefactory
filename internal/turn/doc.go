// Package turn executes one comment.
//
// A turn is the whole business path triggered by a single instruction comment:
// parse it, take the per-task lock, prepare the worktree, run the harness,
// harvest the result. Scheduling ticks, signals and deferred retries belong to
// internal/daemon. Bounded provider-capacity relaunches stay inside the active
// invocation, with injectable cancellable waits; they do not schedule a new turn.
// This package is about what one turn does, so the path is testable without
// wall-clock waits.
//
// [ParseCommand] is the front door: it decides whether a comment body asks for
// anything at all, and is a pure function with no dependencies. [Execute] is
// everything after it.
//
// # What starts a turn
//
// Any owner comment with something in it. The daemon resolves a canonical Mode
// label or invokes its configured default agent as a stateless classifier, then
// passes that concrete mode to [Execute]. The work agent never selects or
// changes its own mode.
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
// # The lock is the cross-process concurrency model
//
// One task, one lock, no queue. The lock is taken before any preparation and
// released on every exit path including a panic (AC-3.1, AC-3.3). A turn that
// finds the lock held does nothing and says so, and the comment stays
// unconsumed so a later tick sees it again. It is durable, it survives a crash,
// and what it excludes is another daemon *process* touching the same worktree
// and the same runs row. Together with the uniqueness of (session, triggering
// comment) on the runs row — which makes a replayed comment a no-op rather than
// a second run (AC-3.4) — that covers two daemons, a crash mid-run, and the
// same comment arriving twice.
//
// What it does not cover is two turns of one process, because that is not this
// package's business. The intra-process model is the daemon's in-flight set and
// its worker pool, and internal/turn deliberately knows nothing about either:
// [Execute] is a function of its arguments that can be called from as many
// goroutines as internal/daemon decides to have.
//
// The one place the two meet is [Options.CloneGate]. Concurrent turns in one
// process share a registered clone, and git contends on it — measured, at
// parallelism 4: three quarters of concurrent fetches failing to lock a
// remote-tracking ref, "worktree add" dying on a sibling's half-written
// registry entry, "push --set-upstream" exiting 0 without writing an upstream.
// Git mutations use two short critical sections: [prepareClone] and
// [commitAndPush]. The agent and managed Plan forge publication run outside
// those sections, so external I/O cannot hold the shared clone gate.
//
// The gate is injected rather than owned here for the same reason the clock is
// not here: internal/daemon decides when turns happen, and how many at once is
// part of when.
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
// # Managed Plan publication
//
// [PublishPlan] validates and publishes complete proposals through the managed
// forge. The factory writes a link-only Story Index before the normal save,
// and marks the Plan ready only after publication and save succeed. Discovery
// of per-story Markdown files is not a publication route.
package turn

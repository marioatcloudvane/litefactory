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
// # What starts a turn
//
// Any owner comment with something in it. A "/spike <text>" runs in spike mode;
// a comment that names no command runs in harness.InferMode, where deciding
// what kind of work is being asked for is part of the task the run is given.
//
// litefactory does not classify the request itself. The worktree a run executes
// in holds the target repository's own CLAUDE.md, and if that file defines
// modes then it is already the authority on how to tell them apart — so the
// prompt points the run at it. Classifying here instead would mean a second
// model call to derive a label the run derives anyway, and would freeze one
// repository's idea of the modes into a tool meant to serve any of them.
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
// # Sub-issues are filed after the run's own push, never before
//
// A Plan run writes plan_brief.md and one file per user story; litefactory
// files those stories as GitHub sub-issues and writes the links back into the
// brief's Story Index. That phase runs between [save] and [finish], and it is
// deliberately last: report.Story.DetailSHA pins each sub-issue's "technical
// detail" link to a commit, and a permalink into a commit that is not yet on
// the remote is a guaranteed 404. So the phase does nothing at all unless the
// run's own commit and push both succeeded, and its write-back is a second
// commit rather than a fold into the run's — a run's report must list what
// claude did, not what litefactory did afterwards.
//
// Nothing the phase does can change the run's own outcome. It never writes
// result.Changes, SHAAfter, CommitSHA, Committed, Pushed, Err, Outcome or
// Reason; a forge outage during creation leaves a succeeded run succeeded.
//
// It never asks what mode the run was in, because it cannot: a free-text
// comment spawns a run in mode infer and claude self-routes by reading
// CLAUDE.md. Presence of the artifacts is the whole test, which is the same
// rule the mode gates use — state is inferred from artifacts, not
// self-reported.
//
// Known hazard, not currently handled: story ids are unique within a plan but
// not across plans. Two Plan sessions on the same task branch on the same day
// resolve to the same session directory, so a story id reused between them
// would collide — the second run's marker matches the first run's sub-issue
// and the story is skipped as already filed. One plan per branch per day is
// the assumption; nothing enforces it.
package turn

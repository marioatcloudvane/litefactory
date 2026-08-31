// Package daemon is litefactory's sense of time.
//
// It owns the tick loop, the singleton lock, rate-limit backoff, cursor
// persistence and shutdown. It owns no business logic at all: what to do about
// one comment belongs to internal/turn, which has no clock and no signals in
// it. The split is a named M0 invariant — daemon is about *when* a turn
// happens, turn is about *what* one turn does — and it is what keeps the whole
// business path testable without waiting for anything.
//
// # One tick
//
// A tick is [Daemon.Tick], and it is an ordinary method rather than something
// only the loop can reach: every acceptance criterion about polling is asserted
// by calling it directly. It has two phases, and the boundary between them is
// where this package became concurrent.
//
// Discovery, on the Tick goroutine:
//
//  1. Ask each registered repository for the issues touched since the cursor —
//     one bounded, logged query per repo, pull requests filtered out (D-A,
//     AC-2.8).
//  2. Union that with the sessions the store says still need a poll, which
//     costs no API quota and is what finds work the listings can no longer
//     reach (RD-2, [store.DB.ListBacklogSessions]).
//  3. For each, find or create the task and name it. Nothing has been acted on
//     yet: what discovery produces is a set of candidates.
//
// Dispatch, still on the Tick goroutine, then a worker per session:
//
//  4. Walk the candidates in two-level round-robin order — repositories rotate,
//     and sessions rotate within a repository — taking a slot from the pool for
//     each and starting a goroutine (RD-1, [Daemon.dispatch]).
//  5. Persist the cursor, but only if the tick completed (AC-2.9, AC-2.13), and
//     return. **The tick does not wait for the turns it started.** That is the
//     whole of AC-PAR-1.1: a thirty-minute run on one issue no longer stops
//     another being noticed thirty seconds later.
//
// Each worker then does one session's work: plant the watermark of a task seen
// for the first time and process none of its existing comments (AC-2.10), or
// fetch the comments above the watermark, walk them in ascending id order
// (AC-2.11), and classify, act on and consume each one.
//
// At most max_concurrent_runs workers run at once, and every one of them is
// joined by the one named termination condition, [Daemon.Quiesce]. A session
// is entered in the daemon's in-memory in-flight set before it is dispatched
// and left only once its report has posted, so two turns for one session can
// never overlap however many are in flight for other sessions.
//
// # A task's comment queue stops at the first comment that was not consumed
//
// This is the one rule in this package that is not obvious and is expensive to
// get wrong. [store.DB.AdvanceWatermark] takes a MAX and never rewinds, so
// consuming comment 12 after failing to report comment 11 puts the watermark
// past 11 for ever: the report is never retried and nothing ever says so. The
// loop therefore checks the watermark itself after each comment — the store,
// not an inference from a return value — and stops that task's queue at the
// first comment the watermark did not move past. Other tasks in the same tick
// carry on, and the session is left carrying sessions.needs_poll so the next
// tick's discovery comes back to it.
//
// The poll cursor is no longer what brings it back. It used to be held to the
// deferred issue's update time, which re-listed that issue and, in doing so,
// rewound discovery for every other repository at the same time. It is a pure
// watermark now — it advances to the tick's start on every tick that completed
// — and the backlog flag carries the memory instead: durable, per session, and
// cross-repo, so one busy issue holds nothing else back (AC-PAR-5.1, AC-PAR-5.2).
//
// That single check is also what makes a contended per-task lock behave as
// AC-3.1 requires: nothing was consumed, so the queue stops and a later tick
// retries, with no queue and no "busy" reply.
//
// # Failure is a state to survive, not a reason to die
//
// A 403 or 429 sets a logged, exponential backoff from the delay GitHub asked
// for; the daemon skips ticks until it expires and then resumes on its own,
// with the cursor and every watermark exactly as they were (AC-2.12). Any
// other transport failure ends the tick without persisting the cursor, so the
// next one redoes the work (AC-2.13). Neither is fatal.
//
// # Shutdown
//
// [Daemon.Run] is a foreground loop. It never forks, never self-daemonizes and
// writes no pid file (AC-2.5): the process is supervised by launchd, and the
// root signal context created in main is the only cancellation source. On
// SIGTERM or SIGINT it stops within [DefaultShutdownGrace] plus change, whether
// or not a run is in flight — an abandoned run is left as it is, and the next
// start reconciles it to failed (abandoned), posts the failure comment and
// advances the watermark, with no re-run (D-B, AC-2.21).
package daemon

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
// A tick is [Daemon.Tick], and it is an ordinary synchronous method rather
// than something only the loop can reach. Every acceptance criterion about
// polling is asserted by calling it directly:
//
//  1. Ask each registered repository for the issues touched since the cursor —
//     one bounded, logged query per repo, pull requests filtered out (D-A,
//     AC-2.8).
//  2. For each issue, find or create the task. A task seen for the first time
//     has its watermark planted at the highest existing comment id and nothing
//     is processed (AC-2.10).
//  3. Fetch that task's comments above the watermark and walk them in
//     ascending id order (AC-2.11).
//  4. Classify each one, act on it, and consume it.
//  5. Persist the cursor, but only if the tick completed (AC-2.9, AC-2.13).
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
// carry on; the tick holds its cursor back to the deferred issue's update time
// so the comment is seen again.
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

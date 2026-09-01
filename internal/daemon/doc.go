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
//     reach (RD-2, [store.DB.ListBacklogSessions]), and again with the
//     sessions still carrying an open issue-body decision — also no API
//     cost — which is what finds a task the owner assigned after litefactory
//     had already looked at it once ([store.DB.ListBodyPendingSessions]; see
//     "An issue body can run once" below).
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
// # The dispatch policy: two-level round-robin
//
// The policy is named here because a fairness rule nobody can find is a rule
// nobody can rely on (AC-PAR-2.1). It is implemented by [Daemon.dispatch],
// which describes the mechanism and points back at this section for what the
// mechanism is for.
//
// Two levels rotate. Repositories rotate against each other: a tick starts from
// the cyclic successor of the repository this daemon last dispatched from, and
// takes one candidate from each in turn. Sessions rotate within a repository:
// each repository is walked from just after the session id it last dispatched,
// ascending by session id and wrapping.
//
// # What it guarantees
//
// With S free slots and R repositories holding eligible work:
//
//   - When S ≥ R, every one of those repositories gets ⌊S/R⌋ or ⌈S/R⌉ slots in
//     the same tick. Nobody waits behind a busier neighbour.
//   - When S < R, the repository cursor advances past the ones already served,
//     so every repository is served within ⌈R/S⌉ ticks. That is the whole of
//     starvation-freedom: a repository's wait is bounded by a number it can be
//     told in advance, whatever the other repositories are doing.
//
// The order comes from the two cursors and from ascending repository id, and
// not from [store.DB.ListRepos] — which sorts by owner, name and path, and
// whose order [Daemon.dispatch] therefore discards. Renaming a repository, or
// changing that ORDER BY, changes nothing about what eventually runs
// (AC-PAR-2.3).
//
// # What it does not promise
//
// Four things, spelled out so that nobody reads more into the guarantee than is
// there:
//
//  1. It is not proportional. A repository with a hundred eligible issues and a
//     repository with one get the same share of the slots. That is a choice:
//     starvation-freedom was preferred to throughput, because the failure it
//     rules out — a busy repository that permanently drowns out a quiet one —
//     is the one an operator cannot work around, and the cost is only that the
//     busy repository drains more slowly.
//  2. It is not preemptive. A thirty-minute run holds its slot for thirty
//     minutes. Round-robin governs who gets a *free* slot and never who keeps a
//     taken one, so with max_concurrent_runs at 3 and three long runs in
//     flight, nothing else starts until one of them ends. That is correct
//     behaviour and not starvation: the bound is on waiting for a *free* slot,
//     and there is no free slot to wait for.
//  3. It is not a priority system. There is no way to make a repository or an
//     issue go first, and none is planned — a priority would be the one input
//     that could reintroduce starvation.
//  4. It is not fair over comments. The rotation is over sessions, so a session
//     with five queued comments is served once per turn of the wheel and not
//     five times. Its comments are then drained in order within that one turn,
//     which is a different guarantee (AC-PAR-1.4) and not this one.
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
// # An issue body can run once, as the task's first instruction — and the question is durable, not a single moment
//
// The comment queue is not the only way a run starts. On a task's first
// sight — before any of its existing comments are read — [Daemon.runIssueBody]
// looks at the issue itself and treats its body as the first instruction, when
// all of these hold: the issue is assigned to the owner, matched on the
// numeric user id and never on a handle; its body is not empty or whitespace;
// and it has no comments yet, because an issue that has already been talked on
// has a conversation to follow instead of a body to replay underneath it.
//
// The gate is assignment, not authorship. Anyone may open an issue, but only
// the owner's own account can assign one to the owner, and assigning it to
// himself is the act of instruction; the issue's author — who may be a
// stranger — is carried through only as the prompt's attribution.
//
// Authorship does decide one narrower question, and it is worth naming
// because two issues that look identical on the thread — same body, same
// comments, same assignee — can behave differently depending on who opened
// them, and that is deliberate rather than an inconsistency to chase down.
// The "no comments yet" half of the gate applies only when somebody other
// than the owner opened the issue (issue #10, User Story 3; AC-2.16d as
// amended). Comments on the owner's own issue are remarks on his own
// request, not a competing conversation to defer to, so they no longer block
// his own body from running; comments on anyone else's issue still do,
// exactly as before. This is an exemption from that one condition, never a
// second way to satisfy assignment: an issue the owner wrote himself but
// never assigned to himself still starts nothing, however it reads. The
// comparison is the numeric author id against the stored owner id, with the
// same zero-id guard [github.Issue.AssignedTo] already uses, so a deleted
// account (author id 0) is never mistaken for the owner's own. The one gap
// this leaves, on purpose: a stranger opens an issue, somebody comments, the
// owner assigns it afterwards — still nothing runs, and a comment is still
// how he gets it moving. That is the case the original rule was written for
// and it is untouched.
//
// Assigning an issue *after* litefactory has already seen it is not a special
// case: the decision is not "ask once, at the instant this task is first
// seen" but "ask, and keep asking until the answer stops being no". A refusal
// made for want of assignment — and only that refusal, and only when the body
// is non-empty, the issue has no comments, and the parsed command spawns
// something — is recorded on the session (sessions.body_pending) and asked
// again on every later tick, because GitHub does not document what bumps an
// issue's updated_at and assignment is not confirmed to be among the actions
// that do: the forge's own listings may never bring this task back on their
// own once its watermark is planted. [Daemon.discover] unions the sessions
// still carrying that flag into what it found, at no API cost, exactly the
// way it already does for [store.DB.ListBacklogSessions]; the flag is what
// [Daemon.work]'s drain arm checks before it asks [Daemon.runIssueBody] a
// second time, so an ordinary session — never refused this way, or already
// settled one way or the other — pays nothing for the possibility. The flag
// clears the moment the body runs, the moment its refusal becomes permanent
// (an empty body, or comments that have since arrived), or the moment the
// issue is found closed. See ".aiteam/AC_AMENDMENTS.md" for the amendment to
// AC-2.16b this durability corrects, and [Daemon.runIssueBody] for the full
// narrowing.
//
// Every branch that declines to run the body logs why, and not all of them at
// the same level. A refusal that follows the owner having pointed litefactory
// at this issue — an empty body, an issue that already has comments, a body
// that parses to nothing to act on, or a named command that spawns nothing —
// logs at Info, because it is the only trace of that decision the owner can
// otherwise not tell apart from litefactory simply not running. The "not
// assigned to the owner" branch stays at Debug: it fires for nearly every open
// issue in nearly every registered repository on nearly every tick
// ([Daemon.sight]), and it is not a refusal in the owner's sense, since nobody
// asked litefactory for anything — even on the ticks where it is being asked
// again for a session already marked body_pending.
//
// # Failure is a state to survive, not a reason to die
//
// A 403 or 429 sets a logged, exponential backoff from the delay GitHub asked
// for; the daemon skips ticks until it expires and then resumes on its own,
// with the cursor and every watermark exactly as they were (AC-2.12). Any
// other transport failure ends the tick without persisting the cursor, so the
// next one redoes the work (AC-2.13). Neither is fatal.
//
// The hold is checked at two points and never at one: at the top of
// [Daemon.Tick], and again immediately before each dispatch, because a worker
// that met a 429 arms it while the dispatch loop is still running (AC-PAR-4.4).
// It gates the *dispatch of new runs* and nothing else: the backoff holds an
// instant, owns no CancelFunc and touches no worker's context, so the turns
// already in flight run to their end and their claude processes are left alone
// (AC-PAR-4.3).
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

# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The production loop calls Tick from a configurable timer. Defaults are a 30-second poll interval and three concurrent workers. Tick discovers and dispatches work without waiting for worker completion; dispatch counts therefore do not prove an agent ran.

Evidence:

- `internal/daemon/daemon.go:730`
- `internal/daemon/daemon.go:763`
- `internal/config/config.go:534`
- `internal/daemon/tick.go:79`

## f2

Ordinary discovery reads registered repositories, the persisted poll cursor, GitHub issue changes and repositories' commented-issue listings. It unions those with sessions marked needs_poll and managed parent records, deduplicating by session. Managed children are handled through their parent rather than as independent candidates. Only supported GitHub repositories participate.

Evidence:

- `internal/daemon/tick.go:194`
- `internal/daemon/tick.go:347`
- `internal/daemon/tick.go:475`
- `internal/daemon/tick.go:522`
- `internal/daemon/story_refinements.go:27`

## f3

The first cursor is planted at the current tick time to avoid replaying history. Later listing windows overlap the saved cursor by five minutes. A completed discovery cycle advances the cursor to tick start; failed or budget-deferred discovery leaves it unchanged. Unfinished session work survives independently in needs_poll, which is cleared only after a clean drain (or retirement of a closed issue's backlog).

Evidence:

- `internal/daemon/tick.go:190`
- `internal/daemon/tick.go:257`
- `internal/daemon/tick.go:311`
- `internal/daemon/tick.go:1093`
- `internal/daemon/tick.go:1160`

## f4

A discovered candidate is not yet an instruction. A worker reads the session comment watermark. First sight seeds it at the highest existing comment ID and skips those comments. The issue body can run on first sight only when assigned to the numeric owner identity, nonempty, comment-free and parsed as a spawning instruction. Later workers process comments above the watermark in ascending ID order. The identity filter excludes self and third-party comments; empty and nonspawning controls do not start normal agent execution.

Evidence:

- `internal/daemon/tick.go:1171`
- `internal/daemon/tick.go:1268`
- `internal/daemon/tick.go:1352`
- `internal/daemon/tick.go:1427`
- `internal/daemon/tick.go:1557`
- `internal/daemon/tick_test.go:299`

## f5

Ordinary candidates rotate across repositories and sessions using in-memory last-dispatched IDs. Admission checks report-recovery holds, the session in-flight set, shared worker capacity, issue availability/open state and rate-limit backoff. A full pool creates no worker queue: needs_poll keeps deferred work discoverable for reconsideration next tick. A session already in flight cannot start a second worker.

Evidence:

- `internal/daemon/tick.go:824`
- `internal/daemon/tick.go:952`
- `internal/daemon/tick.go:1072`
- `internal/daemon/pool.go:69`

## f6

Global rate-limit backoff suppresses new dispatch, including a final check after materializing an ordinary task. Existing workers are not canceled. Delay starts from Retry-After, doubles across consecutive refusals and caps at one hour. Separately, discovery can defer calls for stale quota evidence, reserved quota, traffic budget or waiter priority. Report recovery can hold a particular session until its saved next retry time, or while no retry time is available.

Evidence:

- `internal/daemon/tick.go:158`
- `internal/daemon/tick.go:1038`
- `internal/daemon/backoff.go:50`
- `internal/daemon/discovery_budget.go:114`
- `internal/daemon/report_recovery.go:261`
- `internal/daemon/dispatch_test.go:59`

## f7

A task's comment queue stops at the first comment whose watermark has not advanced. This preserves ordering during a failed report, pending delivery or contended task lock; later comments cannot overtake it. Before execution, mode admission, agent-label selection and permission-profile selection can also refuse or defer work. Closing an ordinary issue suppresses execution and retires its polling backlog; that is a stop condition, not a timed wait.

Evidence:

- `internal/daemon/tick.go:1427`
- `internal/daemon/tick.go:1758`
- `internal/daemon/tick.go:1093`
- `internal/daemon/tick_test.go:299`
- `internal/daemon/tick_test.go:429`

## f8

Scheduled work is selected from pending rows with non-null due_at at or before tick time, ordered by due time then ID, and atomically reserved before dispatch. Dependency schedules wait for successful prerequisites, then use the latest prerequisite finish time plus delay. Missing repository, recovery hold, an in-flight session, full capacity, issue-read failure, closed issue or backoff prevents launch and leaves that schedule pending. Closed scheduled work differs from ordinary backlog retirement.

Evidence:

- `internal/store/schedules.go:119`
- `internal/store/schedules.go:138`
- `internal/store/schedules.go:298`
- `internal/daemon/schedules.go:21`
- `internal/daemon/schedules.go:45`

## f9

After discovery, normal dispatch order is automatic continuations, then schedules, then ordinary candidates, sharing the same worker pool. A continuation requires its due time and valid saved task, worktree, agent, mode and conversation identity. Closure, changed context or a newer actionable owner instruction cancels it; disabling automatic recovery cancels pending continuations. Tick also retries stopped-result debt before its global backoff check, so recovery activity is not identical to new instruction dispatch.

Evidence:

- `internal/daemon/tick.go:136`
- `internal/daemon/tick.go:231`
- `internal/daemon/continuation_dispatch.go:18`
- `internal/daemon/continuation_dispatch.go:92`

## f10

The state boundary is explicit: durable state includes repositories, poll cursor, session needs_poll and comment watermark, managed parent/child records, schedule prerequisites/status/due times, continuation context and report recovery. Runtime state includes tick time/configuration, worker occupancy, in-flight sessions, rotation cursors, rate-limit backoff and discovery budget. Fresh GitHub data supplies issue state, assignment, labels, comments and author identities. This is the existing scheduling machinery the explanation reuses; no new scheduler or schema is requested.

Evidence:

- `internal/daemon/tick.go:194`
- `internal/daemon/tick.go:824`
- `internal/daemon/tick.go:952`
- `internal/daemon/tick.go:1171`
- `internal/daemon/schedules.go:45`
- `internal/daemon/continuation_dispatch.go:92`

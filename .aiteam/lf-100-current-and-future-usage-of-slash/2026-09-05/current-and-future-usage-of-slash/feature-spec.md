# Feature Spec: Current and Future Usage of Slash Commands
# Status: APPROVED
# Date: 2026-09-05

## Goal
- Let repository owners queue work for a later, context-efficient time while Litefactory, rather than a coding agent, owns parsing, persistence, timing, dispatch, and acknowledgement.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As a repository owner, I want obsolete reserved slash commands removed so that Litefactory presents only commands that have a real behavior. |
| US-2 | As a repository owner, I want to schedule a work comment after a relative delay or at an absolute local-or-zoned time so that heavy work can run at an appropriate time. |
| US-3 | As a repository owner, I want later issue comments to continue processing after I schedule work so that a future job does not block the issue queue. |
| US-4 | As a repository owner, I want to make later scheduled work wait for one or more earlier scheduled jobs plus a delay so that I can sequence batches of implementation work. |
| US-5 | As a repository owner, I want Litefactory to request and display an ID-bearing proposed schedule, then accept that exact proposal, so that a coding agent can help sequence a large issue without controlling scheduling. |
| US-6 | As an operator, I want logs to show the outstanding scheduled-work count so that scheduling load is visible beside in-flight and deferred work. |

## Data Model

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| ScheduledWork | ID | integer / durable identifier | Yes | Unique; returned in confirmation; stable across daemon restart. |
| ScheduledWork | repository and issue identity | foreign keys / integers | Yes | Associates work with exactly one registered repository issue. |
| ScheduledWork | source comment identity | integer | Yes | Unique per accepted scheduling control comment; prevents duplicate creation on retry. |
| ScheduledWork | owner identity | string | Yes | Must be the authorized owner that submitted the control comment. |
| ScheduledWork | payload | text | Yes | Original post-`/schedule` work payload only; never includes schedule flags or a nested factory scheduling command. |
| ScheduledWork | due time | UTC timestamp | Conditionally | Required for clock-based schedules; stored as the resolved UTC instant. |
| ScheduledWork | prerequisite IDs | durable relationship | No | All named prerequisites must belong to the same repository and issue and must complete successfully before this work becomes eligible. |
| ScheduledWork | prerequisite delay | duration | Conditionally | Required with `--after`; starts when the final prerequisite succeeds. |
| ScheduledWork | status | enum | Yes | `pending`, `running`, `completed`, `failed`, `cancelled`, or `blocked`; only pending work is counted as outstanding. |
| ScheduledWork | run identity and timestamps | integer / UTC timestamps | No | Set when dispatched and terminalized; supports idempotency and auditing. |
| ScheduleProposal | ID | durable opaque identifier | Yes | Returned to the owner; unique and stable across restart. |
| ScheduleProposal | repository, issue, owner, source comment identity | foreign keys / strings / integer | Yes | Proposal is visible and acceptable only for its originating issue and owner; source comment is unique. |
| ScheduleProposal | items | ordered durable child records | Yes | Each item contains a concrete mode/work payload and a concrete resolved timing or prerequisite sequence. |
| ScheduleProposal | status | enum | Yes | `pending`, `accepted`, `replaced`, `expired`, or `rejected`; only one acceptance is allowed. |
| ScheduleProposal | created, accepted, expiry timestamps | UTC timestamps | Yes / No | Pending proposals expire after a documented finite retention interval. |

### Relationships
- ScheduledWork → repository issue via registered repository and issue keys.
- ScheduledWork → ScheduledWork through zero or more prerequisite IDs.
- ScheduleProposal → repository issue via registered repository and issue keys.
- ScheduleProposal → ordered proposal items via proposal ID.
- Accepted ScheduleProposal → ScheduledWork records via proposal ID.

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Owner issue comment | `/schedule -by HH:MM:SS <work payload>` | Existing owner-command authorization | Creates one pending clock-based item due after a strictly positive `HH:MM:SS` delay from receipt. `HH` is two digits from `00` through `23`; minutes and seconds are `00` through `59`. |
| Owner issue comment | `/schedule -to MM/DD/YYYY HH:MM:SS [ZONE] <work payload>` | Existing owner-command authorization | Creates one pending clock-based item due at the resolved future instant. Omitted `ZONE` means the daemon process's OS-local timezone; supplied `ZONE` is `UTC` or a valid IANA zone such as `Europe/Vienna`. |
| Owner issue comment | `/schedule --after <schedule-id[,schedule-id...]> -by HH:MM:SS <work payload>` | Existing owner-command authorization | Creates one dependency-based item due after every named prerequisite has succeeded, followed by the positive delay. This is the batch-sequencing syntax: task 03 can wait two hours after scheduled task IDs for tasks 01 and 02. |
| Owner issue comment | `/schedule --propose <work description>` | Existing owner-command authorization | Invokes a bounded, factory-controlled agent proposal interaction; no work run is scheduled or started by this command. |
| Owner issue comment | `/schedule --accept <proposal-id>` | Existing owner-command authorization | Atomically converts the exact pending proposal's ordered items into ScheduledWork records. |
| Owner issue comment | `/schedule --reject <proposal-id>` | Existing owner-command authorization | Explicitly discards a pending proposal; included to make the proposal lifecycle closable without overloading acceptance. |
| Daemon tick | Due scheduled-work dispatch | Internal | Selects eligible pending work and dispatches it under existing capacity rules; the coding agent receives the work payload only, not scheduling directives or proposal metadata. |
| Daemon log | `scheduled=<count>` | Internal | Emits the count of pending outstanding ScheduledWork records, including clock-waiting and prerequisite-waiting items, excluding running and terminal items. |

## Business Rules
- The command vocabulary after this change is `/research`, `/plan`, `/implement`, `/validate`, `/spike`, and `/schedule`; `/approve`, `/include`, `/reset`, `/retry`, and `/status` are removed and are treated as unknown slash commands.
- `/schedule` is a Litefactory-only control command: factory code validates and persists it, daemon code decides eligibility, and no coding agent interprets timing flags, proposal acceptance, or dependencies.
- A scheduling control comment is consumed only after its durable schedule/proposal state and its immediate reply have succeeded; consuming it must allow later comments on the same issue to proceed normally.
- A schedule payload starts after the recognized schedule arguments and must be non-empty; it may be a normal work request or one actionable mode command, including `/implement <storyid>`.
- `/implement <storyid>` remains opaque agent instruction handoff; Litefactory does not validate the story ID or plan artifacts.
- A scheduled payload may not itself be `/schedule`, `--propose`, `--accept`, or `--reject`; nested or recursive factory scheduling is refused with an immediate validation reply.
- Exactly one schedule timing form is required: `-by`, `-to`, or `--after` plus `-by`; `-to` and `--after` cannot be combined.
- `-to` rejects malformed dates, invalid zones, non-future instants, and ambiguous or nonexistent wall-clock times caused by daylight-saving transitions; the reply directs the owner to use an unambiguous time or `UTC`.
- `-by` rejects zero, negative, malformed, and out-of-range durations.
- `--after` rejects unknown, terminal-unsuccessful, cross-issue, duplicate, self-referential, or cyclic prerequisite IDs; all prerequisites must be pending or successfully completed schedule records from the same issue.
- A dependency-based item becomes eligible only after all prerequisites succeed; if any prerequisite fails or is cancelled, the dependent item becomes `blocked`, does not start, and produces an immediate factory reply identifying the blocking schedule ID.
- A clock-based item becomes eligible on the first daemon poll at or after its due time and may start later if normal run capacity is unavailable; exact-second execution is not promised.
- Each eligible item allocates at most one work run; restart, repeated polling, and duplicate source delivery must not create duplicate schedules or duplicate runs.
- Scheduling, proposal creation, proposal acceptance, proposal rejection, malformed scheduling input, and dependency blocking each post an immediate factory reply; successful schedule and acceptance replies include durable IDs, resolved due time or dependency rule, and the outstanding scheduled count.
- A proposal agent interaction returns a strictly validated, factory-owned structured proposal containing concrete ordered work items, each with an explicit mode/work payload and exact resolved timing or prerequisite relationships; arbitrary agent prose is not accepted as a schedule.
- Proposal acceptance requires the explicit proposal ID, is restricted to the proposal's originating owner and issue, is atomic, and cannot be repeated; expired, replaced, rejected, or already accepted IDs receive an immediate refusal reply.
- A new proposal for the same issue and owner replaces prior pending proposals, preserving the prior record with `replaced` status and never altering already accepted schedules.
- Pending proposals expire after 7 days; expiry is evaluated by the daemon's injected clock and is reported as an unavailable proposal on later acceptance.
- Proposal generation may describe an existing issue's work and sequence, but agents cannot arm schedules, dispatch work, or choose factory state transitions.

## Edge Cases & Error Handling
- Repeated delivery of the same owner scheduling/proposal comment → return the previously persisted result or safely no-op; never duplicate state, replies, or work runs.
- Daemon restart before due time → reload durable pending items and dispatch them when eligible.
- Daemon restart after acceptance but before dispatch → retain every accepted proposal item as pending scheduled work.
- Later ordinary comment after a future schedule → process independently; it is not held behind the future item.
- Multiple eligible scheduled items → dispatch according to existing capacity behavior and deterministic due-time then durable-ID order.
- Absolute time without zone → resolve using the daemon process OS-local timezone at parsing time and persist only UTC.
- Explicit zone with whitespace or an invalid IANA name → reject rather than guessing.
- Proposal agent failure, timeout, or invalid structured output → persist no proposal and post an immediate factory failure reply; no work is scheduled.
- Proposal with no valid items, unsupported mode, empty payload, impossible timing, or invalid dependency graph → reject the proposal output and post an immediate factory failure reply.
- Owner tries to accept another owner's proposal or an ID from another issue → refuse without disclosing proposal contents.
- Scheduled work cannot start because its issue/repository is no longer eligible → terminalize or block it with an immediate explanatory factory reply and exclude it from future dispatch.

## Out of Scope
- Exact wall-clock wakeups outside the normal daemon polling cycle.
- A user-configurable Litefactory timezone setting; the default is the daemon device's OS-local timezone.
- Natural-language date/time parsing.
- Recurring schedules, calendar integrations, cron expressions, or external scheduler services.
- Agent changes that teach coding agents to parse scheduling commands or let them mutate schedule state.
- Litefactory validation of `/implement` story IDs or plan-artifact existence.
- Retrospective migration or preservation of the removed unused command vocabulary.

## Acceptance Criteria
- [ ] `/approve`, `/include`, `/reset`, `/retry`, and `/status` receive the same unknown-command behavior as any other unrecognized slash command, while the five existing actionable mode commands remain available.
- [ ] An authorized owner can create a durable delayed work item with `/schedule -by HH:MM:SS <payload>` and receives an immediate confirmation containing its schedule ID, resolved due time, and scheduled count.
- [ ] An authorized owner can create a durable absolute-time work item with `/schedule -to MM/DD/YYYY HH:MM:SS [ZONE] <payload>`; omitted zone uses device-local time and explicit `UTC` and valid IANA zones are accepted.
- [ ] Invalid duration, timestamp, zone, missing payload, conflicting flags, nested schedule payload, and unauthorized command attempts create no schedule and receive an immediate refusal.
- [ ] A schedule control comment is consumed after successful persistence and acknowledgement, and a later comment on the same issue can run before the scheduled work becomes due.
- [ ] A due item is started no earlier than its resolved due instant, on the first eligible poll subject to capacity, and is dispatched at most once across duplicate ticks and daemon restarts.
- [ ] `scheduled=<count>` is logged as the number of pending outstanding scheduled-work items and is distinct from in-flight and deferred-session metrics.
- [ ] `/schedule --after <id1,id2> -by 02:00:00 <payload>` creates work that starts only after both referenced schedules succeed and two hours have elapsed after the last success.
- [ ] A failed or cancelled prerequisite prevents dependent work from starting, marks it blocked, and posts an immediate explanation.
- [ ] `/schedule --propose <description>` produces either an immediate structured, ID-bearing proposal reply or an immediate failure reply, without scheduling work.
- [ ] `/schedule --accept <proposal-id>` atomically creates all proposed work items, posts their immediate scheduling confirmation, and cannot be accepted a second time.
- [ ] Only the originating owner on the originating issue can accept or reject a pending proposal; proposal records and accepted schedules survive restart.

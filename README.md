# litefactory
A lightweight Software Factory

## Built-in agents

Agents ship with litefactory and need no team selection or installation. Register
any supported repository with `litefactory repo add /absolute/path/to/project`.

## Automatic recovery after session limits

Automatic quota recovery is disabled by default. To opt in for both Claude Code
and Codex, set the following global option in your litefactory configuration:

```yaml
settings:
  auto_resume_on_session_limit: true
```

Configuration is loaded when the daemon starts; there is no hot reload. Restart
the daemon after changing the setting. Omitting the key or setting it to `false`
disables automatic recovery and leaves manual `/schedule` commands available.
Loading `false` permanently cancels pending automatic recoveries; enabling it
again does not revive them.

Only a verified terminal provider quota refusal with a usable conversation
identity and a reliable future reset time can schedule recovery. The factory
persists the continuation before announcing its absolute UTC `/schedule` time.
That comment describes the saved action; it does not create a second schedule.
Recovery resumes the same conversation, request and worktree. Interrupted partial
work remains uncommitted under the ordinary failure policy.

Claude's exact reset timestamp gets an extra 60 elapsed seconds. Codex's displayed
local minute is interpreted in the machine's local timezone, rounded to its next
minute boundary, then given another 60 elapsed seconds: a displayed 15:00 reset
is eligible at 15:02. Agent timezones are not overridden. Ambiguous dates or DST
times, contradictory timezone information, and missing, malformed or stale reset
times are reported for manual recovery. Persisted UTC deadlines remain unchanged
if the machine's timezone later changes; capacity and normal backoff can delay
launch beyond the deadline.

A recovery chain permits at most **three actual automatic invocations** after the
original interruption. Each further retry needs newly verified quota/reset
evidence; reservation and report-delivery retries do not consume attempts. When
the budget is exhausted, the conversation identity is unusable, or the failure is
not a supported quota refusal, the report requests manual recovery. Automatic
recovery never silently starts a new conversation. New admitted owner work,
conversation reset, changed agent/mode/segment, or observed issue closure cancels
stale recovery permanently. Schedule controls alone do not supersede unrelated
work. After resolving a report-only failure, the owner can issue a new instruction
or manually schedule work on the issue.

## Webhooks and polling

litefactory can react to issues and comments within seconds when GitHub sends it a webhook; polling (`settings.poll_interval_minutes`) stays on as a safety net. Set `webhook.public_url` and run `litefactory repo webhook --all` to register the hooks. See [docs/features/github-webhooks.md](docs/features/github-webhooks.md) for setup, security and troubleshooting.

## Reading polling diagnostics

The INFO tick summary counts workers and forge discovery, using these explicit
names alongside compatible legacy aliases:

| Field | Legacy alias | Meaning |
|---|---|---|
| `workers_dispatched` | `dispatched` | Worker handoffs during this tick, including routine polling. |
| `workers_in_flight` | `in_flight` | Workers held in the in-flight set when the tick finished dispatching. |
| `forge_issues` | `issues` | Issues discovered through forge issue/comment listings; excludes local managed registry and backlog enrollment. |

Each alias has exactly the same value as its explicit counterpart. In-flight
counts are snapshots, not promises about the next instant or counts of prepared
agent runs. Two eligible open managed parents can produce two workers on every
tick even with `forge_issues=0` and no pending backlog: registered parents are
polled so new child input is detected without a parent timestamp change.

At DEBUG, `worker dispatched` names `repo`, canonical `task`, `session_id`, and
`dispatch_sources`: `forge_discovery`, `managed_registry`, `backlog`,
`due_schedule`, or `automatic_continuation`. Sources describe enrollment, not
proof of actionable work. Scheduled and continuation workers also carry their
durable `schedule_id` or `continuation_id`; `tick_at` groups events for the tick.

Each admitted worker normally ends with DEBUG `worker finished`, carrying the
same context, elapsed `duration`, `status`, and `execution_requests`. The latter
counts calls to the executor, including failed calls and lock refusals; it is
not a prepared-run, process-launch, or success count. Zero requests can still
include useful control work such as replies or scheduling. Worker status is
`completed`, `deferred`, `failed`, or `cancelled`, independent of agent success.
The event follows worker body cleanup and in-flight release; pool release comes
after it, so it does not promise a free pool slot. Quiesce joins this diagnostic.
Panics and process death are outside the normal-return guarantee.

`turn complete` remains a per-trigger event: a worker can handle multiple
triggers before it finishes. INFO `run prepared` establishes that a run was
prepared; it does not establish process launch, completion, or success.

For pending or uncertain report delivery, see [report recovery](docs/report-recovery.md).
It describes retained content, bounded retry diagnostics and safe investigation.

## Structured story contracts

[The operator protocol](docs/story-contract.md) describes typed Plan sections,
helper feedback, evidence lists, editable issue reconciliation and explicit legacy
upgrade. Preview an existing unfinished feature with
`litefactory story-contract upgrade github:owner/repo#123`; only `--apply` changes
its contract. Completed history and persisted workflow remain unchanged.

[The instruction manifest](docs/story-contract-instruction-manifest.md) records
shared-source targets, isolated rendering and the manual opposite-harness handoff.
Authentic helper/correction/receipt evidence from both harnesses remains a separate
operator prerequisite; synthetic checks do not establish that runtime capability.

Daily cleanup of finished task worktrees and merged branches, the advisory
`task retire` preview, and retirement logs are described in
[Retiring finished work](docs/retirement.md).

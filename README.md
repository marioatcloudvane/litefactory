# litefactory
A lightweight Software Factory

## Central teams

Team definitions belong to the operator, under `$LF_HOME/teams` (default
`~/.litefactory/teams`). Repositories only record a team selection. Management
commands work outside Git and never install specialist files into a clone or
worktree.

```sh
litefactory team add go-team
litefactory team add my-team --source /absolute/path/to/team-package
litefactory team list --json
litefactory team show go-team --mode research --json
litefactory team validate go-team
litefactory repo add /absolute/path/to/project --team go-team
litefactory team status --json
litefactory team update
```

An import package contains `index.json` using schema version 2, definitions
under `agents/`, and supporting resources under `skills/` or beside their agent.
Every team explicitly declares `research`, `plan`, `implement`, `validate`, and
`spike` arrays; `[]` selects no specialists. Entries use `{"ref":"shared-id"}`
or inline `id`, `desc`, `path`, `tier`, and optional `skills` fields. Neutral
tiers are the existing `fast`, `balanced`, and `flagship` configuration tiers.
The embedded distribution is a complete example.

Imports copy the selected team's complete dependency closure and keep source
provenance in `teams/.litefactory/catalog.json`. Imported packages must live
outside registered clones and their worktree directories. Installed teams remain
readable if an import origin disappears; status/update then report the missing
origin. Older index schemas are rejected; there is no migration command.

Status distinguishes in-sync, locally-modified, upstream-updated, conflict, and
missing files. Update preserves local changes unless `--force` is explicit.
Conflicting shared definitions used by another installed team are refused.

Overrides are operator-global and copied into central storage:

```sh
litefactory team manage shared:research-orchestrator set source=/absolute/path/to/agent.md
litefactory team manage shared:research-orchestrator show --json
litefactory team manage shared:research-orchestrator unset
litefactory team manage team:go-team:go-architect show
```

An override must retain the target's frontmatter identity, description and tier.
Declared relative Markdown/resource dependencies travel with it; registry skill
dependencies resolve from `skills/` beside the override file. A shared override
affects explicit shared references only. Base updates retain overrides, and
unsetting restores the updated base definition. Conflicting changes to an
already installed supporting skill are refused. Unused old override files are
retained for recoverability. Legacy `--repo` and `--all-worktrees` management
options are removed. Repository markers remain diagnostic and do not select an
external team.

**Runtime delivery is not enabled by this change.** Central storage, validation
and repository association are preparatory: immutable per-run CLI bundles,
workflow instruction cleanup, and token benchmarks remain gated on verified
runner capabilities. Claude scratch probes established selected external
agent/skill loading; Codex verification was blocked by the execution sandbox.
Runs for repositories with an external team selected fail with an actionable
transport-unavailable error before invoking a CLI. Existing repositories without
an external team assignment keep their current runtime behavior.

### GitHub story progress

Managed plans keep complete user stories in GitHub sub-issues. Planning comments
on a story refine the parent plan. Promotion to Implement freezes selected story
revisions; subsequent comments receive a locked notice and do not alter scope.
The factory supplies the same approved bodies to cold and resumed executions.
Detached, missing or moved stories block a later dispatch; live body edits are
reported without silently replacing approved requirements.

Each story closes as soon as its own acceptance checks, review and tests have
verified it, while other stories can still be running. If later work invalidates
that evidence, the affected story reopens. Final feature checks remain required.
Comment `replan` or `/plan <instruction>` on the parent while no run is active
to reopen unfinished scope. Existing legacy story files are not migrated or converted.

### Confirming conflict resolutions

Conflict suggestions resolve factory-identified regions. Litefactory preserves
the surrounding file bytes and validates every reconstructed file before storing
a proposal and again before applying an owner's numeric confirmation. Legacy
whole-file proposals cannot be applied: request a new suggestion, review it, and
confirm its new proposal number.

The preview names the task-branch baseline SHA and shows every file's raw
before/after byte counts, logical lines, and additions/deletions. Its diff is
sanitized and may be incomplete; omission never removes the statistics. If the
complete metadata cannot fit the comment limit, no confirmable proposal is
created.

A result strictly below half the baseline bytes **or** half its logical lines is
refused. Exactly half is allowed; an empty baseline alone does not trigger this
check. Go files must parse as complete source, but this is not type checking.
Other languages receive content checks, not language-specific parsing. Invalid
UTF-8, NUL, remaining markers, unsupported conflict types, and incomplete region
coverage require manual resolution or regeneration; there is no unsafe-confirm
override.

Collection accepts at most 100 files, 4,096 bytes of complete conflict content
per file and the configured total content budget. Original files are read within
a 1 MiB bound, and aggregate advisor output is bounded to 1 MiB. Entire proposals
are refused when a bound or supported-content requirement fails. Confirmation
rechecks raw digests, statistics, file modes, and written/staged bytes before
committing. Failed restoration is reported explicitly; a push failure after
commit can leave the local merge commit in place.
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

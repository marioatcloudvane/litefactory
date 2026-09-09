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
litefactory team manage shared:codebase-tour set source=/absolute/path/to/agent.md
litefactory team manage shared:codebase-tour show --json
litefactory team manage shared:codebase-tour unset
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

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

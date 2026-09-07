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

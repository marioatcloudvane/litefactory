# Teams

Teams supply the mode-specific agent roster and supporting skills. Install them once in the operator's central catalog under `$LF_HOME/teams`, then associate repositories using `repo add --team`. Management works outside a Git checkout and does not populate the registered clone's instruction directories.

## Install and inspect

Prerequisites: a configured [team source](../configuration/config-yaml.md), network access for HTTPS downloads and any configured credential reference. Local imports must be independent packages outside registered repositories and worktree storage.

```sh
litefactory team add TEAM --source SOURCE_ALIAS
litefactory team show TEAM
litefactory team validate TEAM --mode implement
litefactory repo add /absolute/path/to/clone --team TEAM
```

`team add` reports the installed team and catalog directory. `list`, `show` and `validate` inspect installed definitions, mode rosters, tiers, byte counts and digests; `--json` exposes deterministic structured detail. Inspection validates captures and reports missing or invalid resources. `--mode` accepts `research`, `plan`, `implement`, `validate` or `spike`.

A source is a configured alias, HTTPS index URL or explicit `./`, `../` or absolute package path. Bare names always mean aliases, even if a local directory has the same name. An omitted source uses `teams.default_source` when configured, otherwise `teams.index_url`. Local imports reject remote version/digest pins.

## Versions, updates and drift

Without `--version`, remote installs follow the stable-release policy; `--version 1.2.3` selects an exact semantic version. `--sha256 DIGEST` adds an expected manifest digest. Installation records the source URL, alias, content version and manifest digest. Retargeting an alias later does not redirect an installed team's recorded origin.

```sh
litefactory team status TEAM
litefactory team status TEAM --refresh
litefactory team update TEAM
```

Ordinary status uses retained observations; `--refresh` checks upstream without installing. Output distinguishes local drift and upstream freshness, including installed/candidate/latest-stable versions and last successful observation. Missing or failed freshness is not proof that the installed version is current. Drift can produce exit code `3` after the findings are printed.

Update uses recorded origins; omit `TEAM` to update installed teams. Exact versions remain pinned. To change a version, name one team and use `--version`; replace its digest with `--sha256`, or explicitly remove that digest with `--clear-pin`. Both digest operations require `--version`; they cannot be combined. Switching source requires both `--source` and `--switch-source` on a named installed team.

States include `in-sync`, `upstream-updated`, `locally-modified`, `conflict` and `missing`. Inspect and preserve local edits before using `--force`: it permits replacing modified/conflicting installed sources. It does not grant access or bypass package validation. Catalog changes during acquisition are refused and require a fresh attempt. See the [complete flag reference](command-reference.md#teams).

## Operator-global overrides

Overrides replace an agent source centrally, not per repository. Use `shared:ID` for a shared agent or `team:TEAM:AGENT` for a team-owned one; select IDs shown by inspection.

```sh
litefactory team manage team:TEAM:AGENT set source=/absolute/independent/agent.md
litefactory team manage team:TEAM:AGENT show
litefactory team manage team:TEAM:AGENT unset
```

`set` requires `source=PATH`; `show` and `unset` reject it. Output identifies the effective path and origin; `--json` is available. Overrides preserve a base definition, and upstream updates retain the override while refreshing that base. Invalid source metadata, paths inside registered clone/worktree storage and unknown scopes fail explicitly.

## What a run receives

The factory captures the installed team's selected mode and renders an immutable per-run instruction bundle with source hashes and provenance. Startup and runs do not automatically fetch team updates. Retained bundles are verified before reuse; edited or missing bytes fail instead of silently loading newer sources. Changed mode or instruction identity rotates the conversation segment. Automatic quota recovery refuses a changed instruction bundle and requires manual continuation.

These are implemented delivery paths, not a claim of authentic current-host verification for #274/#296. See [coding agents](../configuration/coding-agents.md), [MCP](../guides/mcp.md) and [recovery](../operations/recovery.md).

[CLI index](README.md) · [Repositories](repositories.md)

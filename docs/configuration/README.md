# Configuration — advanced

[Documentation](../README.md) · [CLI](../cli/README.md)

Configure factory behavior, coding agents and permission boundaries here. GitHub issue controls belong in [Using the factory](../guides/README.md).

| Topic | Use it to |
| --- | --- |
| [config.yaml reference](config-yaml.md) | Look up every field, default, range and precedence rule |
| [Coding agents](coding-agents.md) | Select Claude or Codex and configure models and timeouts |
| [Permissions](permissions.md) | Configure credential references, mode permissions and personal configuration |

## Locate and initialize

Complete [installation](../getting-started/installation.md) and [GitHub setup](../getting-started/github-setup.md) first. `litefactory init` creates `config.yaml` under `$LF_HOME`, defaulting to `~/.litefactory`. A nonblank relative `LF_HOME` is resolved to an absolute directory; use the same home for CLI and daemon. Initialization preserves an existing configuration rather than replacing it with new defaults.

The generated file includes both agent stanzas, the model table and all four permission profiles. Keep only agents you have installed and authenticated, removing the matching cells from every permission profile as well. An absent `agents` mapping does not configure an implicit agent. The first declared agent is the default.

## Change a setting

Edit your existing file, preserving its other fields. For example, to reduce simultaneous runs:

```yaml
settings:
  max_concurrent_runs: 1
```

This is an edit fragment, not a complete configuration. Restart the daemon through the [supported lifecycle commands](../operations/running-the-daemon.md), then inspect status and logs. Configuration is loaded at process startup; editing the file does not hot-reload the running daemon. CLI processes that load configuration see the new file on their next invocation.

Unknown keys, multiple YAML documents, invalid ranges, incomplete permission matrices and recognized literal credentials are refused. Diagnostics name the file and affected keys; independent validation failures are reported together. Fix those errors before restarting again. There is no dedicated `config validate` command.

## Understand precedence

- Omitted scalar fields use decoder defaults; explicit `false` and `0` are preserved and then validated. Zero is valid for `auto_sync_max_behind`, but invalid for polling and timeout values.
- Repository registration resolves flags, then global defaults, then repository-derived defaults. Stored registrations retain resolved values; changing YAML does not relocate existing worktrees. See [repositories](../cli/repositories.md).
- An issue's coding-agent label overrides the configured default. Mode selects its permission profile; profiles do not inherit a missing agent cell.
- Explicit team source selection overrides `teams.default_source`, which overrides `teams.index_url`. See [teams](../cli/teams.md).

The following pages describe executable behavior at source revision `8f556367c2d0a00d2736a53a326152ff43a6f96e`. Source and offline checks do not establish authentic host delivery for #274/#296, fresh installation or successful remote publication.

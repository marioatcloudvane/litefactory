# Teams

A team is a versioned package of agent instructions (roles and skills) for the workflow modes. Teams are installed once per machine, under `$LF_HOME/teams`, and then linked to the repositories that should use them. The commands work from any folder and do not put files into a registered clone.

Without a link, a repository uses the built-in workflows. Teams are not required.

## Install and look at teams

```sh
litefactory team install NAME
litefactory team list
litefactory team inspect NAME VERSION
```

The standard catalogue offers one version of each team, 1.0.0. Versions stay at 1.0.0 until the owner decides otherwise.

`team install` downloads the team from the catalogue, checks it, and installs it. Leave out the version to get the highest stable one. It says `Already installed` if you have it. `team list` shows what is installed, from local files only. `team inspect` checks one installed version and shows its folder, checksum and workflows.

The catalogue address is `settings.team_source` in `config.yaml`. Its default is the standard catalogue. It must be an HTTPS address or an absolute file path (see [config.yaml](../configuration/config-yaml.md)). Use `--source` to pick a different catalogue for one command. Where a team name and version exist in more than one source, add `--source` so the command knows which one you mean.

## Update

```sh
litefactory team update NAME
```

This installs the newest stable version from the same catalogue if it is newer than the one you have. Older versions stay installed next to it, so repositories linked to them are not changed. Linked repositories keep their link until you link them to the new version.

## Make your own version

```sh
litefactory team clone NAME VERSION NEW-NAME NEW-VERSION
```

`team clone` copies an installed team into an editable local team named `NEW-NAME` at `NEW-VERSION`. It prints the folder to edit. Local teams live in the `local` source, are not downloaded again, and cannot be updated. Use `--source local` to name them in other commands.

## Link a team to a repository

```sh
litefactory team link NAME VERSION --repo /absolute/path/to/clone
litefactory team unlink --repo /absolute/path/to/clone
```

The repository must already be [registered](repositories.md). Without `--repo`, the command uses the repository in your current folder. `team link` replaces any earlier link. `team unlink` returns the repository to the built-in workflows. Both are safe to repeat.

## Remove

```sh
litefactory team remove NAME VERSION
```

A team version that is still linked to a repository is not removed; the command lists the repositories. Unlink them first.

## What a run receives

When a run starts, litefactory reads the team linked to the repository. Starting the daemon or a run does not download team updates. See [coding agents](../configuration/coding-agents.md), [MCP](../guides/mcp.md) and [recovery](../operations/recovery.md).

Every flag and exit code is in the [command reference](command-reference.md#teams).

[CLI index](README.md) · [Repositories](repositories.md) · [Documentation index](../README.md)

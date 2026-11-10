# Teams

A team is a versioned package of agent instructions (roles and skills) for the workflow modes. Teams are installed once per machine, under `$LF_HOME/teams`, and then linked to the repositories that should use them. The commands work from any folder and do not put files into a registered clone.

Without a link, a repository uses the built-in workflows. Teams are not required.

## Install and look at teams

```sh
litefactory team install NAME --source /absolute/path/to/litefactory/teams/source
litefactory team list
litefactory team inspect NAME VERSION
```

Teams are installed straight from a `teams/source` folder on this machine, usually the one in your litefactory checkout. There is no download and no catalogue. Each team is at version 1.0.0, read from its `release.json`. Versions stay at 1.0.0 until the owner decides otherwise.

`team install` builds `teams/source/NAME` (with its base and its `inherit.json` values), checks the workflows and installs it. It prints `Installed`, `Already installed` (nothing changed) or `Replaced`, the checksum, and the source commit (with a note when the folder had uncommitted changes). `team list` shows what is installed, from local files only. `team inspect` checks one installed version and shows its folder, checksum, workflows and source commit.

`--source` is the absolute path of the `teams/source` folder. Without it, the command uses `settings.team_source` in `config.yaml` (see [config.yaml](../configuration/config-yaml.md)); when that is empty, `--source` is required. A web address or a `teams.json` file is refused: give the folder.

On the other commands, `--source` names an installed source: a source folder, an old catalogue address from an earlier install, or `local`. Where a team name and version exist in more than one source, add `--source` so the command knows which one you mean.

## Refresh after a source change

Run `team install` again. When the files changed, the installed version is replaced in place and every repository linked to it moves to the new files at once; the command prints a `Relinked:` line for each. Tasks already running keep the files they started with. There is no `team update`.

## Make your own version

```sh
litefactory team clone NAME VERSION NEW-NAME NEW-VERSION
```

`team clone` copies an installed team into an editable local team named `NEW-NAME` at `NEW-VERSION`. It prints the folder to edit. Local teams live in the `local` source; `team install` refuses a name and version that already exist there. Use `--source local` to name them in other commands.

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

When a run starts, litefactory reads the team linked to the repository. Starting the daemon or a run does not read `teams/source`; only `team install` does. See [coding agents](../configuration/coding-agents.md), [MCP](../guides/mcp.md) and [recovery](../operations/recovery.md).

Every flag and exit code is in the [command reference](command-reference.md#teams).

[CLI index](README.md) · [Repositories](repositories.md) · [Documentation index](../README.md)

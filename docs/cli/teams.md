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

## Select deny groups in a local team

Clone an installed team and link the editable copy, substituting your registered clone path:

```sh
litefactory team clone go-development 1.0.0 restricted-local 1.0.0
litefactory team link restricted-local 1.0.0 --source local --repo /absolute/path/to/clone
```

In your existing operator `config.yaml`, merge this fragment under `settings`:

```yaml
settings:
  denyrules:
    full_checks:
      deny:
        - 'Bash(make check:*)'
        - 'Bash(scripts/lf-check.sh:*)'
    private_files:
      deny:
        - 'Read(secrets/**)'
        - 'Edit(generated/**)'
    no_reviewer:
      deny:
        - 'Agent(reviewer)'
    no_delegation:
      deny:
        - 'Agent'
```

These are example commands and paths for your project; they need not exist in a shipped team. Edit the applicable workflow DOT in the folder printed by `team clone`. Add a graph attribute and attributes to existing `lf_agent` nodes, retaining their roles, outcomes and edges:

```dot
// Inside the existing digraph:
lf_deny="full_checks";
worker [lf_deny="private_files,no_reviewer,full_checks"];
other_worker [lf_deny=""];
```

Use your actual agent node names in place of `worker` and `other_worker`. The first node gets `full_checks`, `private_files`, then `no_reviewer`; the repeated group is removed. The empty node still gets `full_checks`. Selections contain group names only. Never put `lf_deny` on a terminal, control or judge node, even with an empty value. Shipped teams remain unchanged and contain no group references.

Use Claude for the file and named-agent selections above. For Codex 0.160.0, keep the `full_checks` graph default and replace the first node selection with `worker [lf_deny="no_delegation"];`. That delivers both literal command-prefix bans and disables all native delegation; use an empty node selection when delegation should remain allowed. Do not select `private_files` or `no_reviewer` for Codex: either refuses the entire launch, including the supported entries. See [named deny groups](../configuration/permissions.md#named-deny-groups) for the supported native forms, worktree and absolute path anchors, matching limits and refusal recovery. New and resumed selected attempts read current operator settings; active attempts retain their rules through retries and cold fallback. Other configuration changes still need a daemon restart.

Try the documented denied and unrelated allowed actions with your supported runtime, including a permitted native descendant and resume. For Codex, use the existing file-based login and check that adding, changing and removing groups preserves your conversation and usage records; unsupported credential modes refuse before launch. This is a live owner check; automated launch tests do not establish native enforcement. Correct an unknown name, malformed rule or unsupported runtime selection using the launch diagnostic, then retry.

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

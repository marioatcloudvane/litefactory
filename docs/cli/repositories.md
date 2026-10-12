# Repositories

Register an existing local clone so litefactory can poll its GitHub issues and create separate worktrees for each task. Registration reads the clone and stores its settings. It does not fetch from the network, change the clone's files or install anything in the clone.

## Register and list

You need a finished `litefactory init` (see [Getting started](../getting-started/README.md)), Git on your `PATH`, and a non-bare clone whose `origin` points to GitHub.

```sh
litefactory repo add /absolute/path/to/clone
litefactory repo list
```

`repo add` prints `registered` or `updated`, then the path, remote, base branch (and where it came from), branch prefix and worktree folder. It then tries to register the repository's GitHub webhook if `webhook.public_url` is set in `config.yaml`. If that fails, you get a warning and the registration still stands. You can retry later with `litefactory repo webhook OWNER/REPO`.

`repo list` shows each repository's path, remote, base branch, worktree folder and last fetch time (UTC, or `never` when none has been recorded).

## Options and defaults

For a first registration, the first value that exists wins:

| Value | Order, highest first |
| --- | --- |
| Base branch (`--base`) | The flag, then `settings.default_base_branch`, then the clone's cached `origin/HEAD`. |
| Branch prefix (`--prefix`) | The flag, then `settings.feature_prefix`, then `lf`. |
| Worktree folder (`--worktrees`) | The flag, then `settings.worktree_root`, then `.lf-worktrees/<clone-folder-name>` next to the clone. |

A relative worktree folder becomes absolute using the folder you ran the command from. Each task gets its own folder under it, named `task-<issue-number>`. Two repositories cannot share a worktree folder.

Running `repo add` again for the same clone updates it. A flag you leave out keeps the value saved earlier, so editing `config.yaml` later does not change an existing registration. Pass the flag to change it. See the [command reference](command-reference.md#initialization-and-repositories) for every flag and the exit codes, and [config.yaml](../configuration/config-yaml.md) for the settings.

## Choosing a team

Registration does not choose a team. Without a choice, the repository uses the built-in workflows. To use an installed team, link it with `litefactory team link`. See [Teams](teams.md).

## Common problems

| Symptom | What to do |
| --- | --- |
| Not initialized, or no database | Run `litefactory init`. |
| Missing or unsupported `origin`, or a bare repository | Register the working clone that has a GitHub `origin`. |
| Unknown base branch | Pass `--base`, for example when `origin/HEAD` is not cached. |
| Worktree folder already used | Give each repository a different `--worktrees` folder. |
| Webhook warning | Check `webhook.public_url` and the bot token, then run `litefactory repo webhook OWNER/REPO`. |

There is no command to remove a repository from the list. Removing the worktrees of finished tasks is a separate job: see [cleanup](../operations/cleanup.md).

[CLI index](README.md) · [Teams](teams.md) · [Documentation index](../README.md)

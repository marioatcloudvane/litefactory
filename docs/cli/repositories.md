# Repositories

Register an existing local clone so litefactory can poll its GitHub issues and create separate task worktrees. Registration reads the clone and stores its settings; it does not fetch, rewrite clone files or install team instructions there.

## Register and inspect

Prerequisites: completed [initialization](../getting-started/README.md), Git on `PATH`, a non-bare clone with a supported GitHub `origin`, and an [installed central team](teams.md). Existing aiteam installations remain a compatibility path when no external team is selected.

```sh
litefactory repo add /absolute/path/to/clone --team TEAM --base main
litefactory repo list
```

Registration reports `registered` or `updated`, the remote, resolved base branch and source, prefix, worktree root and team. Listing shows path, remote, base, detected aiteam team, external team, worktrees and last fetch. `never` means no fetch has been recorded; timestamps are UTC.

## Options and precedence

| Value | First registration, highest priority first |
| --- | --- |
| Base branch | `--base`, `settings.default_base_branch`, locally cached `origin/HEAD`. |
| Branch prefix | `--prefix`, `settings.feature_prefix`, `lf`. |
| Worktree root | `--worktrees`, `settings.worktree_root`, `<clone-parent>/.lf-worktrees/<clone-directory-name>`. |
| External team | `--team TEAM`; selected team must capture successfully in all five modes. |

Relative worktree roots become absolute against the command's current directory. Task directories use `task-<issue-number>` beneath that root. Different registrations cannot share an effective worktree directory.

Repeating `repo add` for the same clone updates its row. Omitted flags retain the previously stored team, base, worktree root and prefix before consulting global defaults. A later config edit therefore does not reset an existing registration. Supply explicit values to change it; an empty `--team` does not clear an existing association. See [all flags](command-reference.md#initialization-and-repositories) and [YAML defaults](../configuration/config-yaml.md).

## Common failures

| Symptom | Action |
| --- | --- |
| No config/database | Complete `litefactory init`. |
| Missing or unsupported origin, bare repository | Register the intended working clone with a GitHub origin. |
| Unknown base branch | Supply `--base` when cached `origin/HEAD` is absent. |
| Missing aiteam or external team | Install a central team, then register with `--team`. |
| Team is not ready | Inspect `team validate TEAM`; fix the catalog before registration. |
| Worktree-root collision | Choose a distinct `--worktrees` path for each repository. |

No repository removal command is currently registered. Task-resource removal is a separate [cleanup operation](../operations/cleanup.md), not deregistration.

[CLI index](README.md) · [Teams](teams.md)

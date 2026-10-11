# Configuration

[Documentation home](../README.md) · [CLI](../cli/README.md)

Configure factory behavior, coding agents and permission boundaries here. GitHub issue controls belong in [Using the factory](../guides/README.md).

| Topic | Use it to |
| --- | --- |
| [config.yaml reference](config-yaml.md) | Look up every key: type, default, range, effect, and the environment variables the factory reads |
| [Coding agents](coding-agents.md) | Choose Claude or Codex and set models and time limits |
| [Permissions](permissions.md) | Set credential references and what each mode may do |

## Where the file is

Finish [installation](../getting-started/installation.md) and [GitHub setup](../getting-started/github-setup.md) first. `litefactory init` writes `config.yaml` under `$LF_HOME` (default `~/.litefactory`) and keeps an existing file as it is. Use the same `LF_HOME` for the CLI and the daemon; see [environment variables](config-yaml.md#environment-variables).

The generated file has both agents, the model table and the permission profiles. Keep only the agents you have installed and signed in to, and remove that agent's cell from every permission profile too. The first agent listed is the default.

## Change a setting

Edit the existing file and keep its other keys. For example, to run one task at a time:

```yaml
settings:
  max_concurrent_runs: 1
```

This is a fragment, not a whole file. The daemon reads the file only when it starts, so stop and start it afterwards (see [running the daemon](../operations/running-the-daemon.md)). Other commands read the file each time you run them.

A bad file stops the load. Unknown keys, a second YAML document, out-of-range numbers, an incomplete permission setup and anything that looks like a pasted credential are refused, and the message names the key. All problems are listed together. `litefactory doctor` also loads the file; see [doctor](../operations/doctor.md). There is no separate command to validate the file.

## Which value wins

- A key you leave out gets its default. A key you write is checked; for example `0` is fine for `auto_sync_max_behind` and refused for `poll_interval_minutes`.
- For a new repository, `repo add` flags win over `config.yaml`, which wins over values found from the repository. Existing registrations keep the values they were stored with; changing the YAML does not move existing worktrees. See [repositories](../cli/repositories.md).
- An issue's coding-agent label wins over the default agent. The mode picks the permission profile.
- `--level` on `daemon run` or `daemon start` wins over `settings.log_level`.

[Documentation home](../README.md)

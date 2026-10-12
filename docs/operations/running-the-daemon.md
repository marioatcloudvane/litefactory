# Running the daemon

[Documentation home](../README.md) · [Operations](README.md)

Use these commands on the initialized macOS host with its configured credentials, Git and selected coding-agent executables available. Repository registration is covered in [Getting started](../getting-started/README.md).

## Lifecycle

```sh
litefactory daemon start
litefactory daemon status
```

`start` installs or replaces the per-user LaunchAgent and loads it in `gui/$UID`. It captures the invoking shell's PATH, pins `LF_HOME`, and records this executable's absolute path. Invoke it from the intended installation. The agent runs at load/login; it is not a system LaunchDaemon and has no KeepAlive restart loop after a fatal exit.

`status` reports the plist, loaded/running state, PID when available and last completed tick (`never` before the first). Loaded does not necessarily mean running. Worker dispatch and executor-request counters describe admission activity, not guaranteed provider launches.

`litefactory daemon stop` unloads the agent but leaves its plist installed. Stopping an already stopped agent succeeds; the installed agent can load at a later login. `litefactory daemon start` loads it again and replaces/reloads an existing job. For a deliberate configuration or executable update, finish active work, stop, update, then start and inspect status/logs. There is no `daemon restart` command. Do not overwrite an executing binary; follow [installation and upgrades](../getting-started/installation.md).

Configuration and credentials are read once, when the daemon starts; nothing reloads while it runs. After editing `config.yaml`, run `daemon stop` and `daemon start`. Re-run `start` as well after changing the shell PATH or an environment-referenced GitHub token; changing your terminal environment alone does not change the loaded agent. A `providers.github.token_env` token is copied into the private plist and the command warns about this. Every key and the environment variables the factory reads are in the [config.yaml reference](../configuration/config-yaml.md); see also [permissions](../configuration/permissions.md).

On start, the daemon checks that `git` and the configured agent executables are found, so a wrong binary path fails at once instead of hours into a run. A second daemon refuses to start because the first holds a lock on `$LF_HOME/daemon.lock`.

## What the daemon does

- **Agents are built in.** There are no team files to install for the daemon to run. Register a project with `litefactory repo add /absolute/path/to/project`.
- **Polling and webhooks.** The daemon lists GitHub every `settings.poll_interval_minutes` as a safety net. If `webhook.public_url` is set, `litefactory repo webhook --all` registers hooks so issues and comments are seen within seconds. See [GitHub setup](../getting-started/github-setup.md) and the [webhook guide](../getting-started/github-setup.md).
- **Validate.** The owner starts Validate with `/validate` after Implement finishes (see [owner commands](../guides/owner-commands.md)). Set `validate.auto_start: true` to have the factory start it itself.
- **Quota recovery.** Off by default; see [recovery](recovery.md).
- **Daily housekeeping.** Old worktrees, logs, archives and database backups are cleaned up; see [cleanup](cleanup.md).
- **Health check.** `litefactory doctor` inspects the installation; see [doctor](doctor.md). Usage and cost data is in the [meter](../meter.md).

For foreground diagnosis, stop the managed instance first, then use `litefactory daemon run --level debug`. It runs until interrupted; a second instance refuses the exclusive daemon lock. `--level` on `run` or `start` overrides configured logging for that invocation without editing YAML.

## Read logs

```sh
litefactory logs -n 100 --level warn
litefactory logs 'github:OWNER/REPO#123' --runs
litefactory logs 'github:OWNER/REPO#123' --run 2 --only result
```

Replace the repository and issue placeholders. With no issue, `logs` reads the app log, showing the last 200 records by default. `-n`, `--all` and minimum `--level` apply only to this view. Filtering cannot recover debug records that were never captured.

With an issue, the default is its newest run's summary and rendered events. `--runs` lists runs; `--run N` selects an ordinal; `-f` follows the run. A full task ref or `--repo OWNER/REPO` disambiguates repeated issue numbers. `--only` selects event kinds (`agent` is a useful combined view); `--full` expands shortened tool arguments/output. Thinking is hidden unless requested. `--grep`, `-i`, `--invert` and `--raw` support further inspection; raw output preserves JSONL bytes. See the [CLI reference](../cli/command-reference.md) for combinations and kinds.

## Reading the tick summary

Each poll writes an INFO line with these fields (older names remain as aliases with the same values):

| Field | Alias | Meaning |
| --- | --- | --- |
| `workers_dispatched` | `dispatched` | Workers started during this tick, including routine polling. |
| `workers_in_flight` | `in_flight` | Workers still running when the tick finished starting new ones. |
| `forge_issues` | `issues` | Issues found through GitHub listings. It excludes issues the factory already tracks locally. |

Two open parent issues can start two workers on every tick even when `forge_issues=0`, because registered parents are polled to catch new child input. At DEBUG, `worker dispatched` and `worker finished` name the repo, task, session and where the work came from (`forge_discovery`, `managed_registry`, `backlog`, `due_schedule` or `automatic_continuation`); `worker finished` also gives the duration and status (`completed`, `deferred`, `failed` or `cancelled`). `run prepared` means a run was prepared, not that it started or succeeded. A worker count is not a count of agent launches.

## Upgrading a story contract

`litefactory story-contract upgrade PARENT-REF` previews the upgrade of an unfinished legacy feature; `--apply` makes the change. See the [CLI reference](../cli/command-reference.md).

Logs work while the daemon is stopped, provided the database and files exist. Output is **unredacted**, unlike outbound reports. Missing event files, missing runs and ambiguous issue numbers have distinct diagnostics; use [troubleshooting](troubleshooting.md). If startup fails, inspect app logs for invalid configuration, inaccessible credentials or unavailable binaries before starting again.

[Documentation home](../README.md) · [Operations](README.md) · [Recovery](recovery.md) · [Troubleshooting](troubleshooting.md)

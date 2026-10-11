# Running the daemon

Use these commands on the initialized macOS host with its configured credentials, Git and selected coding-agent executables available. Repository registration is covered in [Getting started](../getting-started/README.md).

## Lifecycle

```sh
litefactory daemon start
litefactory daemon status
```

`start` installs or replaces the per-user LaunchAgent and loads it in `gui/$UID`. It captures the invoking shell's PATH, pins `LF_HOME`, and records this executable's absolute path. Invoke it from the intended installation. The agent runs at load/login; it is not a system LaunchDaemon and has no KeepAlive restart loop after a fatal exit.

`status` reports the plist, loaded/running state, PID when available and last completed tick (`never` before the first). Loaded does not necessarily mean running. Worker dispatch and executor-request counters describe admission activity, not guaranteed provider launches.

`litefactory daemon stop` unloads the agent but leaves its plist installed. Stopping an already stopped agent succeeds; the installed agent can load at a later login. `litefactory daemon start` loads it again and replaces/reloads an existing job. For a deliberate configuration or executable update, finish active work, stop, update, then start and inspect status/logs. There is no `daemon restart` command. Do not overwrite an executing binary; follow [installation and upgrades](../getting-started/installation.md).

Configuration and credentials are read at startup. Re-run `start` after changing the shell PATH or an environment-referenced token; changing your terminal environment alone does not change the loaded agent. An environment token is materialized in the private plist and the command warns about this. See [permissions](../configuration/permissions.md) and [configuration](../configuration/README.md).

For foreground diagnosis, stop the managed instance first, then use `litefactory daemon run --level debug`. It runs until interrupted; a second instance refuses the exclusive daemon lock. `--level` on `run` or `start` overrides configured logging for that invocation without editing YAML.

## Read logs

```sh
litefactory logs -n 100 --level warn
litefactory logs 'github:OWNER/REPO#123' --runs
litefactory logs 'github:OWNER/REPO#123' --run 2 --only result
```

Replace the repository and issue placeholders. With no issue, `logs` reads the app log, showing the last 200 records by default. `-n`, `--all` and minimum `--level` apply only to this view. Filtering cannot recover debug records that were never captured.

With an issue, the default is its newest run's summary and rendered events. `--runs` lists runs; `--run N` selects an ordinal; `-f` follows the run. A full task ref or `--repo OWNER/REPO` disambiguates repeated issue numbers. `--only` selects event kinds (`agent` is a useful combined view); `--full` expands shortened tool arguments/output. Thinking is hidden unless requested. `--grep`, `-i`, `--invert` and `--raw` support further inspection; raw output preserves JSONL bytes. See the [CLI reference](../cli/command-reference.md) for combinations and kinds.

Logs work while the daemon is stopped, provided the database and files exist. Output is **unredacted**, unlike outbound reports. Missing event files, missing runs and ambiguous issue numbers have distinct diagnostics; use [troubleshooting](troubleshooting.md). If startup fails, inspect app logs for invalid configuration, inaccessible credentials or unavailable binaries before starting again.

[Operations](README.md) · [Recovery](recovery.md)
